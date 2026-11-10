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
	GitHubToken string  `json:"github_token,omitempty"`
	HomeLat     float64 `json:"home_lat"` // centre of the saved offline map area
	HomeLon     float64 `json:"home_lon"`
}

const (
	defaultPort    = 8791
	configFileName = "aegis-companion.json"
	appVersion     = "1.7.7"
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

// localLANIPs lists this machine's private IPv4 addresses, most likely
// home-network address first, so Settings can prefill the desktop.txt host.
func localLANIPs() []string {
	var out []string
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
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			if ip4.IsPrivate() {
				out = append(out, ip4.String())
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		// 192.168.x first - the overwhelmingly common home LAN.
		return strings.HasPrefix(out[i], "192.168.") && !strings.HasPrefix(out[j], "192.168.")
	})
	return out
}

func preferredLANIP() string {
	ips := localLANIPs()
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0]
}
