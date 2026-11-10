package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config mirrors the old Python companion.json so existing SD cards and
// habits carry over unchanged: same port, same desktop.txt format.
type Config struct {
	Port    int    `json:"port"`
	Token   string `json:"token"`
	Library string `json:"library"`
	// UpdateFeed is the URL of the release latest.json the updater
	// checks (v1.7.1: the AEGIS-Desktop GitHub repo by default).
	UpdateFeed string `json:"update_feed"`
	// GitHubToken optionally authenticates feed/download requests
	// when the repo is private. Entered locally in Settings; never
	// leaves this machine except to api.github.com/github.com.
	GitHubToken   string  `json:"github_token,omitempty"`
	HomeLat       float64 `json:"home_lat"` // centre of the saved offline map area
	HomeLon       float64 `json:"home_lon"`
	HomeZip       string  `json:"home_zip,omitempty"`        // as typed by the user (v1.8.0)
	HomePlace     string  `json:"home_place,omitempty"`      // resolved place name for display
	HomeAccuracyM float64 `json:"home_accuracy_m,omitempty"` // estimated radius around HomeLat/HomeLon (v1.8.4)
	HomeSource    string  `json:"home_source,omitempty"`     // ZIP / typed / GPS-refined (v1.8.4)
	MinToTray     bool    `json:"min_to_tray,omitempty"`     // close = hide to system tray (v1.8.1)
	HomeSSID      string  `json:"home_ssid,omitempty"`       // PC presence: your home Wi-Fi name (v1.8.3)
	HomeDevices   string  `json:"home_devices,omitempty"`    // PC presence: "Name=MAC" lines (v1.8.3)
}

const (
	defaultPort    = 8791
	configFileName = "aegis-companion.json"
	appVersion     = "1.8.7"
)

// exeDir is the folder the program lives in. Config, Library and logs sit
// next to the executable so the whole install is one portable folder the
// Start Menu / app-menu shortcut can point at.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func configPath() string { return filepath.Join(exeDir(), configFileName) }

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// loadOrCreateConfig returns the saved config, creating a fresh one (with
// a fresh token) on first run.
func loadOrCreateConfig() (*Config, error) {
	p := configPath()
	if data, err := os.ReadFile(p); err == nil {
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.Token != "" {
			if cfg.Port == 0 {
				cfg.Port = defaultPort
			}
			if cfg.Library == "" {
				cfg.Library = filepath.Join(exeDir(), "Library")
			}
			if cfg.UpdateFeed == "" {
				cfg.UpdateFeed = defaultUpdateFeed
			}
			return &cfg, nil
		}
	}
	cfg := &Config{
		Port:       defaultPort,
		Token:      newToken(),
		Library:    filepath.Join(exeDir(), "Library"),
		UpdateFeed: defaultUpdateFeed,
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	p := configPath()
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return err
	}
	// os.WriteFile honours umask on first create; tighten explicitly.
	_ = os.Chmod(p, 0o600)
	return nil
}

// desktopTxt is the exact /AEGIS_LINK/desktop.txt the firmware reads.
func (c *Config) desktopTxt(host string) string {
	return fmt.Sprintf("host=%s\nport=%d\ntoken=%s\n", host, c.Port, c.Token)
}

// lanCandidate is one private IPv4 address on this machine.
type lanCandidate struct {
	ip    string
	iface string
}

// lanCandidates enumerates private IPv4s with their interface names.
// Multi-network PCs (wired + Wi-Fi + VPN + Docker bridges) routinely
// have half a dozen; picking blindly wrote the WRONG subnet into
// AEGIS_LINK/desktop.txt in the field (wired 192.168.x written while
// the devices lived on Wi-Fi 10.x - v1.8.0 fix).
func lanCandidates() []lanCandidate {
	var out []lanCandidate
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || !ip4.IsPrivate() {
				continue
			}
			out = append(out, lanCandidate{ip: ip4.String(), iface: ifc.Name})
		}
	}
	return out
}

// scoreLAN ranks candidates: an address on the same /24 as a known
// (found) device wins outright; Wi-Fi interfaces beat wired; virtual
// bridges / VPN tunnels sink. Deterministic tie-break keeps the old
// "first listed" behaviour for equal scores.
func scoreLAN(c lanCandidate, deviceIPs []string) int {
	score := 0
	for _, d := range deviceIPs {
		if same24(c.ip, d) {
			score += 100
		}
	}
	name := strings.ToLower(c.iface)
	switch {
	case strings.HasPrefix(name, "wlan"), strings.HasPrefix(name, "wlp"),
		strings.Contains(name, "wi-fi"), strings.Contains(name, "wifi"),
		strings.Contains(name, "wireless"):
		score += 20
	case strings.HasPrefix(name, "docker"), strings.HasPrefix(name, "br-"),
		strings.HasPrefix(name, "veth"), strings.HasPrefix(name, "virbr"),
		strings.Contains(name, "tailscale"), strings.HasPrefix(name, "tun"),
		strings.HasPrefix(name, "wg"), strings.HasPrefix(name, "tap"):
		score -= 50
	}
	return score
}

func same24(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	if len(pa) != 4 || len(pb) != 4 {
		return false
	}
	return pa[0] == pb[0] && pa[1] == pb[1] && pa[2] == pb[2]
}

func rankedLANIPs(deviceIPs []string) []string {
	cands := lanCandidates()
	sort.SliceStable(cands, func(i, j int) bool {
		return scoreLAN(cands[i], deviceIPs) > scoreLAN(cands[j], deviceIPs)
	})
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.ip)
	}
	return out
}

// localLANIPs lists this machine's private IPv4 addresses, most likely
// home-network address first (ranked: device subnet, then Wi-Fi).
func localLANIPs() []string { return rankedLANIPs(nil) }

func preferredLANIP() string {
	ips := rankedLANIPs(nil)
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0]
}

// preferredLANIPFor picks the address devices should be told to reach:
// the first ranked address sharing a subnet with any found device.
func preferredLANIPFor(deviceIPs []string) string {
	ips := rankedLANIPs(deviceIPs)
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0]
}
