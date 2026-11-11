package main

// Network census (v1.8.8): the PC keeps a quiet list of the other
// devices on the home network - name, IP, MAC, a vendor guess from
// the MAC prefix, and first/last seen - shown in its own box on the
// Overview tab below Recent reports/activity. Read-only: it reads the
// kernel neighbour table and (gently) reverse-resolves names. First
// seen times persist across restarts in netcensus.json next to the
// library so "new device on your network" stays meaningful.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
)

type netDev struct {
	Name   string    `json:"name"`
	IP     string    `json:"ip"`
	MAC    string    `json:"mac"`
	Vendor string    `json:"vendor"`
	First  time.Time `json:"first"`
	Last   time.Time `json:"last"`
	Aegis  string    `json:"aegis,omitempty"` // program name when it is one of ours
}

type netCensus struct {
	mu      sync.Mutex
	devs    map[string]*netDev // by MAC (or IP when no MAC)
	started bool
	path    string
}

var gNet = &netCensus{devs: map[string]*netDev{}}

var ouiVendors = map[string]string{
	"240AC4": "Espressif (ESP32)", "30AEA4": "Espressif (ESP32)", "7CDFA1": "Espressif (ESP32)",
	"EC64C9": "Espressif (ESP32)", "48E729": "Espressif (ESP32)", "C45BBE": "Espressif (ESP32)",
	"DCA632": "Raspberry Pi", "B827EB": "Raspberry Pi", "E45F01": "Raspberry Pi", "2CCF67": "Raspberry Pi",
	"001CB3": "Apple", "F0DBE2": "Apple", "A45E60": "Apple", "3C0754": "Apple", "D0817A": "Apple",
	"8C8590": "Apple", "ACBC32": "Apple", "F827B6": "Apple", "0017F2": "Apple", "A82066": "Apple",
	"E8508B": "Samsung", "5C3C27": "Samsung", "001632": "Samsung", "F025B6": "Samsung",
	"3D6AD3": "Google", "F4F5D8": "Google", "001A11": "Google", "D83ADD": "Google/Nest",
	"FCA183": "Amazon", "44D453": "Amazon", "6854FD": "Amazon", "B47C9C": "Amazon (Echo/Ring)",
	"0024E4": "Sonos", "5CAAFD": "Sonos", "949F3E": "Sonos",
	"C8E9EE": "Amazon (Ring)", "9C8E99": "Amazon (Ring)",
	"001D6A": "LG", "A816B2": "LG", "7C2F80": "LG",
	"D8A01D": "TP-Link", "50C7BF": "TP-Link", "1C3BF3": "TP-Link",
	"04D4C4": "Netgear", "A021B7": "Netgear", "9C3DCF": "Netgear",
	"0018E7": "Cisco/Linksys", "60735C": "Asus", "AC220B": "Asus", "049226": "Asus",
	"E8ABFA": "Compal/Dell", "F8B156": "Dell", "84E716": "Microsoft", "001DD8": "Microsoft",
	"001B21": "Intel", "8C8D28": "Intel", "3CFDFE": "Intel",
	"B4E62D": "Xiaomi", "64CC2E": "Xiaomi", "7811DC": "Xiaomi",
	"E0798D": "Bosch/Siemens", "001E8C": "Asustek",
	"00259E": "Bose", "08DF1F": "Bose",
	"18B430": "Nest/Google", "641666": "Nest/Google",
}

func vendorGuess(mac string) string {
	m := strings.NewReplacer(":", "", "-", "").Replace(strings.ToUpper(mac))
	if len(m) >= 6 {
		if v, ok := ouiVendors[m[:6]]; ok && v != "" {
			return v
		}
	}
	// Locally-administered (randomised) MACs: second hex digit 2/6/A/E.
	if len(m) >= 2 {
		switch m[1] {
		case '2', '6', 'A', 'E':
			return "private/randomised MAC"
		}
	}
	return "unknown vendor"
}

var arpLine = regexp.MustCompile(`(\d{1,3}(?:\.\d{1,3}){3})\s+(?:at\s+)?(?:\(.*?\)\s+at\s+)?([0-9a-fA-F]{1,2}(?::[0-9a-fA-F]{1,2}){5})`)

// neighTable returns ip -> mac from the OS neighbour/ARP tables.
func neighTable() map[string]string {
	out := map[string]string{}
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/proc/net/arp"); err == nil {
			for _, ln := range strings.Split(string(b), "\n") {
				f := strings.Fields(ln)
				if len(f) >= 4 && net.ParseIP(f[0]) != nil {
					if _, err := net.ParseMAC(f[3]); err == nil && f[3] != "00:00:00:00:00:00" {
						out[f[0]] = strings.ToUpper(f[3])
					}
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if b, err := exec.Command("arp", "-a").CombinedOutput(); err == nil {
		for _, m := range arpLine.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = strings.ToUpper(m[2])
		}
	}
	return out
}

func startNetCensus(core *Core) {
	gNet.mu.Lock()
	if gNet.started {
		gNet.mu.Unlock()
		return
	}
	gNet.started = true
	gNet.path = filepath.Join(filepath.Dir(core.Store.Root()), "netcensus.json")
	if b, err := os.ReadFile(gNet.path); err == nil {
		var saved []*netDev
		if json.Unmarshal(b, &saved) == nil {
			for _, d := range saved {
				key := d.MAC
				if key == "" {
					key = d.IP
				}
				gNet.devs[key] = d
			}
		}
	}
	gNet.mu.Unlock()
	go func() {
		for {
			scanNetOnce(core)
			time.Sleep(45 * time.Second)
		}
	}()
}

func scanNetOnce(core *Core) {
	tab := neighTable()
	now := time.Now()
	changed := false
	gNet.mu.Lock()
	for ip, mac := range tab {
		key := mac
		d, ok := gNet.devs[key]
		if !ok {
			d = &netDev{IP: ip, MAC: mac, Vendor: vendorGuess(mac), First: now}
			gNet.devs[key] = d
			changed = true
		}
		if d.IP != ip || now.Sub(d.Last) > time.Minute {
			changed = true
		}
		d.IP, d.Last = ip, now
		if d.Vendor == "" {
			d.Vendor = vendorGuess(mac)
		}
	}
	gNet.mu.Unlock()
	// AEGIS devices by name (from discovery), merged live.
	if core.Disco != nil {
		gNet.mu.Lock()
		for _, fd := range core.Disco.Found() {
			for _, d := range gNet.devs {
				if fd.IP != "" && d.IP == fd.IP && fd.Prog != "" {
					d.Aegis = fd.Prog
				}
			}
		}
		gNet.mu.Unlock()
	}
	// Name resolution for newcomers only (in this goroutine, calmly).
	gNet.mu.Lock()
	var todo []*netDev
	for _, d := range gNet.devs {
		if d.Name == "" && now.Sub(d.First) < 24*time.Hour {
			todo = append(todo, d)
		}
	}
	gNet.mu.Unlock()
	for _, d := range todo {
		if names, err := net.LookupAddr(d.IP); err == nil && len(names) > 0 {
			gNet.mu.Lock()
			d.Name = strings.TrimSuffix(names[0], ".")
			gNet.mu.Unlock()
			changed = true
		}
	}
	if changed && gNet.path != "" {
		gNet.mu.Lock()
		list := make([]*netDev, 0, len(gNet.devs))
		for _, d := range gNet.devs {
			list = append(list, d)
		}
		gNet.mu.Unlock()
		if b, err := json.Marshal(list); err == nil {
			tmp := gNet.path + ".tmp"
			if os.WriteFile(tmp, b, 0o644) == nil {
				_ = os.Rename(tmp, gNet.path)
			}
		}
	}
}

func netSnapshot() []netDev {
	gNet.mu.Lock()
	defer gNet.mu.Unlock()
	out := make([]netDev, 0, len(gNet.devs))
	for _, d := range gNet.devs {
		out = append(out, *d)
	}
	// Stable order (v1.8.11): by IP address, never by recency. The box
	// used to re-sort on every scan, so the list scrolled/jumped under
	// the reader. Now rows stay put, fields refresh in place, and a
	// new device simply appears in address order with its NEW tag.
	ipKey := func(ip string) uint32 {
		p := net.ParseIP(ip).To4()
		if p == nil {
			return 0xFFFFFFFF
		}
		return binary.BigEndian.Uint32(p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := ipKey(out[i].IP), ipKey(out[j].IP)
		if a != b {
			return a < b
		}
		return out[i].MAC < out[j].MAC
	})
	return out
}

// networkBox renders the census on the Overview tab.
func (u *UI) networkBox(gtx layout.Context) layout.Dimensions {
	devs := netSnapshot()
	return bgFill(gtx, colPanel, 10, pad(12, func(gtx layout.Context) layout.Dimensions {
		rows := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.bold(13, colText, "Other devices on your network").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 4) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(11, colFaint, "From this PC's neighbour table - read-only, nothing is probed or pinged beyond what the OS already knows. First seen is remembered between restarts, so a brand-new name here is worth a look.").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vSpace(gtx, 6) }),
		}
		if len(devs) == 0 {
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(12, colDim, "No neighbours learned yet - this fills in over the first minute as the PC talks to your router and devices.").Layout(gtx)
			}))
		}
		for i, d := range devs {
			if i >= 14 {
				rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.label(11, colFaint, fmt.Sprintf("+ %d more in netcensus.json next to the library", len(devs)-14)).Layout(gtx)
				}))
				break
			}
			d := d
			rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					name := d.Name
					if name == "" {
						name = "(no name)"
					}
					if d.Aegis != "" {
						name = d.Aegis + " (AEGIS)"
					}
					newTag := ""
					if time.Since(d.First) < 24*time.Hour {
						newTag = " · NEW"
					}
					return u.label(11.5, colText, fmt.Sprintf("%s · %s · %s · %s · first %s · last %s%s",
						name, d.IP, d.MAC, d.Vendor,
						d.First.Format("2 Jan 15:04"), d.Last.Format("15:04"), newTag)).Layout(gtx)
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	}))
}
