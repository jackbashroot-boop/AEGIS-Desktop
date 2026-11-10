package main

// PC presence sensing (v1.8.3, user request): the Companion uses
// THIS computer's Wi-Fi radio two ways -
//  1) MOTION: received signal strength of your home networks ripples
//     when bodies move through the house (the poor-man's CSI).
//     We track RSSI per home access point, learn the calm baseline,
//     and report QUIET / MOTION with a history graph.
//  2) WHO'S HOME: devices you name (Settings, "Name=MAC" lines)
//     are looked up on the LAN (ARP neighbours + ping sightings);
//     Cardputers found by Discovery count automatically.
// Scanning shells out to the OS (nmcli on Linux, netsh on Windows) -
// no root, no special drivers. RSSI via an OS scan is coarse, so the
// language stays honest: "motion-like ripple", never "person seen".

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type apSample struct {
	BSSID string
	SSID  string
	RSSI  int // dBm (negative)
}

type presPoint struct {
	At    time.Time
	Score float64
}

type homePerson struct {
	Name string
	MAC  string
	Home bool
	Via  string // "arp" / "aegis"
	Seen time.Time
}

type presenceEngine struct {
	mu       sync.Mutex
	series   map[string][]int // bssid -> recent RSSI (home SSIDs only)
	lastSeen map[string]time.Time
	points   []presPoint
	calm     float64
	calmN    int
	score    float64
	motion   bool
	people   []homePerson
	aps      int
	lastScan time.Time
	lastErr  string
	started  bool
}

var gPresence = &presenceEngine{series: map[string][]int{}, lastSeen: map[string]time.Time{}, calm: -1}

func parseHomeDevices(text string) []homePerson {
	var out []homePerson
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		name, mac, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		mac = strings.ToUpper(strings.TrimSpace(mac))
		if _, err := net.ParseMAC(mac); err != nil {
			continue
		}
		out = append(out, homePerson{Name: strings.TrimSpace(name), MAC: mac})
	}
	return out
}

// scanWiFiAPs returns nearby access points via the OS tool.
func scanWiFiAPs(ctx context.Context) ([]apSample, error) {
	if runtime.GOOS == "windows" {
		out, err := exec.CommandContext(ctx, "netsh", "wlan", "show", "networks", "mode=bssid").CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("netsh scan failed (Wi-Fi on?)")
		}
		return parseNetsh(string(out)), nil
	}
	out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "BSSID,SSID,SIGNAL", "dev", "wifi", "list", "--rescan", "auto").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nmcli scan failed (NetworkManager/Wi-Fi on?)")
	}
	return parseNmcli(string(out)), nil
}

// parseNmcli parses `nmcli -t -f BSSID,SSID,SIGNAL` (SIGNAL is %).
// BSSID colons are backslash-escaped in terse mode.
func parseNmcli(s string) []apSample {
	var out []apSample
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		ln := sc.Text()
		// fields: AA\:BB\:...:SSID:87  - split from the right twice
		i := strings.LastIndex(ln, ":")
		if i < 0 {
			continue
		}
		sig, err := strconv.Atoi(strings.TrimSpace(ln[i+1:]))
		if err != nil {
			continue
		}
		rest := ln[:i]
		j := strings.LastIndex(rest, ":")
		// BSSID may contain escaped colons; SSID is after the 17-char MAC + ':'
		ssid := ""
		bssid := rest
		if len(rest) > 18 {
			// find unescaped split: MAC is first 6 escaped octets
			parts := splitNmcliLine(rest)
			if len(parts) >= 2 {
				bssid, ssid = parts[0], parts[1]
			}
		} else if j >= 0 {
			bssid, ssid = rest[:j], rest[j+1:]
		}
		bssid = strings.ReplaceAll(bssid, `\`, "")
		out = append(out, apSample{BSSID: strings.ToUpper(bssid), SSID: ssid, RSSI: sig/2 - 100})
	}
	return out
}

func splitNmcliLine(s string) []string {
	var parts []string
	var cur strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == ':':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

func parseNetsh(s string) []apSample {
	var out []apSample
	var cur apSample
	flush := func() {
		if cur.BSSID != "" {
			out = append(out, cur)
		}
		cur = apSample{}
	}
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "SSID") && strings.Contains(ln, " : "):
			flush()
			cur.SSID = strings.TrimSpace(strings.SplitN(ln, " : ", 2)[1])
		case strings.HasPrefix(ln, "BSSID"):
			if cur.BSSID != "" {
				flush()
			}
			cur.BSSID = strings.ToUpper(strings.TrimSpace(strings.SplitN(ln, " : ", 2)[1]))
		case strings.HasPrefix(ln, "Signal"):
			v := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(ln, " : ", 2)[1]), "%")
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				cur.RSSI = n/2 - 100
			}
		}
	}
	flush()
	return out
}

// startPresence begins the scan loop (idempotent).
func startPresence(core *Core) {
	gPresence.mu.Lock()
	if gPresence.started {
		gPresence.mu.Unlock()
		return
	}
	gPresence.started = true
	gPresence.mu.Unlock()
	go presenceLoop(core)
}

func presenceLoop(core *Core) {
	time.Sleep(3 * time.Second)
	for {
		cfg := core.Cfg
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		aps, err := scanWiFiAPs(ctx)
		cancel()
		gPresence.mu.Lock()
		gPresence.lastScan = time.Now()
		if err != nil {
			gPresence.lastErr = err.Error()
			gPresence.mu.Unlock()
			time.Sleep(20 * time.Second)
			continue
		}
		gPresence.lastErr = ""
		gPresence.aps = len(aps)
		home := strings.TrimSpace(cfg.HomeSSID)
		now := time.Now()
		var deltas []float64
		seenHome := map[string]bool{}
		for _, ap := range aps {
			if home == "" || ap.SSID != home {
				continue
			}
			seenHome[ap.BSSID] = true
			ser := gPresence.series[ap.BSSID]
			if n := len(ser); n > 0 {
				d := float64(ap.RSSI - ser[n-1])
				if d < 0 {
					d = -d
				}
				deltas = append(deltas, d)
			}
			ser = append(ser, ap.RSSI)
			if len(ser) > 40 {
				ser = ser[len(ser)-40:]
			}
			gPresence.series[ap.BSSID] = ser
			gPresence.lastSeen[ap.BSSID] = now
		}
		// forget APs not seen for 5 minutes
		for b, t := range gPresence.lastSeen {
			if now.Sub(t) > 5*time.Minute && !seenHome[b] {
				delete(gPresence.series, b)
				delete(gPresence.lastSeen, b)
			}
		}
		// ripple score: mean absolute step across home APs, scaled.
		score := 0.0
		if len(deltas) > 0 {
			sum := 0.0
			for _, d := range deltas {
				sum += d
			}
			score = (sum / float64(len(deltas))) * 14 // ~3 dB avg step -> ~42
			if score > 100 {
				score = 100
			}
		}
		if gPresence.calm < 0 {
			gPresence.calm, gPresence.calmN = score, 1
		} else if score < gPresence.calm+18 { // only learn calm from quiet samples
			gPresence.calm = gPresence.calm*0.92 + score*0.08
			gPresence.calmN++
		}
		gPresence.score = score
		gPresence.motion = gPresence.calmN >= 4 && score > gPresence.calm+22 && score > 30
		gPresence.points = append(gPresence.points, presPoint{At: now, Score: score})
		if len(gPresence.points) > 120 {
			gPresence.points = gPresence.points[len(gPresence.points)-120:]
		}
		// who's home: named devices via ARP table + AEGIS sightings.
		people := parseHomeDevices(cfg.HomeDevices)
		neigh := arpTable()
		found := map[string]FoundDevice{}
		if core.Disco != nil {
			for _, fd := range core.Disco.Found() {
				if fd.MAC != "" {
					found[strings.ToUpper(fd.MAC)] = fd
				}
			}
		}
		for i := range people {
			mac := people[i].MAC
			if t, ok := neigh[mac]; ok {
				people[i].Home, people[i].Via, people[i].Seen = true, "arp", t
			} else if fd, ok := found[mac]; ok {
				people[i].Home, people[i].Via, people[i].Seen = true, "aegis", fd.LastSeen
			}
		}
		// Cardputers are people too (unnamed entries appended).
		for mac, fd := range found {
			name := fd.Prog
			if name == "" {
				continue
			}
			dup := false
			for _, p := range people {
				if p.MAC == mac {
					dup = true
				}
			}
			if !dup {
				people = append(people, homePerson{Name: name, MAC: mac, Home: true, Via: "aegis", Seen: fd.LastSeen})
			}
		}
		sort.Slice(people, func(i, j int) bool { return people[i].Name < people[j].Name })
		gPresence.people = people
		gPresence.mu.Unlock()
		time.Sleep(15 * time.Second)
	}
}

// arpTable reads the kernel neighbour table (no root needed).
func arpTable() map[string]time.Time {
	out := map[string]time.Time{}
	b, err := exec.Command("ip", "neigh", "show").CombinedOutput()
	if err != nil {
		return out
	}
	now := time.Now()
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		for i, tok := range f {
			if tok == "lladdr" && i+1 < len(f) {
				if _, err := net.ParseMAC(f[i+1]); err == nil {
					out[strings.ToUpper(f[i+1])] = now
				}
			}
		}
	}
	return out
}

type presenceView struct {
	Score   float64
	Calm    float64
	Motion  bool
	APs     int
	LastErr string
	Last    time.Time
	People  []homePerson
	Points  []presPoint
	Ready   bool
}

func presenceSnapshot() presenceView {
	gPresence.mu.Lock()
	defer gPresence.mu.Unlock()
	return presenceView{
		Score: gPresence.score, Calm: gPresence.calm, Motion: gPresence.motion,
		APs: gPresence.aps, LastErr: gPresence.lastErr, Last: gPresence.lastScan,
		People: append([]homePerson(nil), gPresence.people...),
		Points: append([]presPoint(nil), gPresence.points...),
		Ready:  gPresence.calmN >= 4,
	}
}
