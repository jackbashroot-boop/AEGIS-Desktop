package main

// Field readiness snapshot (v1.8.4 "spice"): one plain-English,
// hand-back-able report that answers "is the kit actually ready?"
// without exposing secrets. It deliberately separates measured facts
// (versions, counts, GPS uncertainty) from advice, and it never
// includes the device token, Wi-Fi passwords, or GitHub credentials.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type readinessData struct {
	Now       time.Time
	OS        string
	Arch      string
	Cfg       *Config
	Health    []HealthItem
	Map       MapData
	Presence  presenceView
	Devices   []DeviceLive
	Staged    fwFeedInfo
	HasStaged bool
	Manifests []string // SD library manifest summaries (v1.8.7)
}

func readinessBool(ok bool) string {
	if ok {
		return "OK"
	}
	return "CHECK"
}

func buildReadinessReport(d readinessData) string {
	cfg := d.Cfg
	if cfg == nil {
		cfg = &Config{}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "AEGIS FIELD READINESS REPORT\n")
	fmt.Fprintf(&b, "Generated: %s\n", d.Now.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Companion: %s (protocol AEGIS/1) on %s/%s\n", appVersion, d.OS, d.Arch)
	fmt.Fprintf(&b, "Library: %s\n", cfg.Library)
	fmt.Fprintf(&b, "Device port: %d\n\n", cfg.Port)

	b.WriteString("LINK HEALTH\n")
	if len(d.Health) == 0 {
		b.WriteString("- No health snapshot was available.\n")
	}
	for _, h := range d.Health {
		status := "OK"
		if !h.OK {
			status = "FAIL"
		} else if h.Warn {
			status = "WARN"
		}
		fmt.Fprintf(&b, "- [%s] %s: %s\n", status, h.Label, h.Detail)
	}

	b.WriteString("\nHOME + LOCATION QUALITY\n")
	if cfg.HomeLat == 0 && cfg.HomeLon == 0 {
		b.WriteString("- Home location: NOT SET. ZIP centre is roughest; exact typed coordinates are better; GPS-refined from several sightings around the property is best.\n")
	} else {
		src := cfg.HomeSource
		if src == "" {
			src = "saved coordinates (source not recorded)"
		}
		fmt.Fprintf(&b, "- Home location: %.5f, %.5f\n", cfg.HomeLat, cfg.HomeLon)
		fmt.Fprintf(&b, "- Home source: %s\n", src)
		if cfg.HomeAccuracyM > 0 {
			fmt.Fprintf(&b, "- Home estimated radius: ±%.0f m\n", cfg.HomeAccuracyM)
		} else {
			b.WriteString("- Home estimated radius: not recorded (treat as approximate).\n")
		}
		if cfg.HomeAccuracyM >= 1000 {
			b.WriteString("- Advice: this is area-level. Wardrive around the property from several sides with GPS lock, sync, then use Settings -> Refine home from GPS sightings.\n")
		}
	}
	fmt.Fprintf(&b, "- Wardrive networks with GPS estimates: %d\n", len(d.Map.Points))
	fmt.Fprintf(&b, "- Route points / distance: %d / %.2f km\n", len(d.Map.Track), d.Map.RouteKm)
	fmt.Fprintf(&b, "- Sightings with no GPS fix: %d\n", d.Map.NoGPS)
	if len(d.Map.Points) > 0 {
		uncs := make([]float64, 0, len(d.Map.Points))
		multi, tight, loose := 0, 0, 0
		for _, p := range d.Map.Points {
			if p.UncertaintyM > 0 {
				uncs = append(uncs, p.UncertaintyM)
			}
			if p.Observations >= 3 {
				multi++
			}
			if p.UncertaintyM > 0 && p.UncertaintyM <= 50 {
				tight++
			}
			if p.UncertaintyM > 500 {
				loose++
			}
		}
		sort.Float64s(uncs)
		if len(uncs) > 0 {
			fmt.Fprintf(&b, "- Network pin uncertainty: median ±%.0f m, best ±%.0f m, worst ±%.0f m\n", uncs[len(uncs)/2], uncs[0], uncs[len(uncs)-1])
		}
		fmt.Fprintf(&b, "- Pins built from 3+ fixes: %d; pins at ±50 m or better: %d; pins looser than ±500 m: %d\n", multi, tight, loose)
		if loose > 0 {
			b.WriteString("- Advice: loose pins improve when the network is seen from several positions with a clear sky view. One GPS point can be wrong; repeated strong fixes out-vote it.\n")
		}
	}

	b.WriteString("\nDEVICES\n")
	if len(d.Devices) == 0 {
		b.WriteString("- No live device snapshot. Finding a device is not linking; linking still requires approval on the device.\n")
	}
	for _, dev := range d.Devices {
		seen := "never"
		if !dev.LastSeen.IsZero() {
			seen = dev.LastSeen.Format("15:04:05")
		}
		fmt.Fprintf(&b, "- %s fw %s: state=%s, battery=%s, APs=%s, events=%d, frames=%d, last seen %s\n",
			dev.Prog, dev.Ver, dev.State,
			map[bool]string{true: fmt.Sprintf("%d%%", dev.Batt), false: "not reported"}[dev.HasBatt],
			map[bool]string{true: fmt.Sprint(dev.APs), false: "not reported"}[dev.HasAPs],
			dev.Events, dev.Frames, seen)
		if dev.HasGPSAcc {
			fmt.Fprintf(&b, "  GPS estimate: ±%d m", dev.GPSAcc)
			if dev.HasGPSRej {
				fmt.Fprintf(&b, ", %d impossible jump(s) rejected", dev.GPSRej)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\nFIRMWARE + PRESENCE\n")
	if d.HasStaged {
		sha := d.Staged.SHA256
		if len(sha) > 12 {
			sha = sha[:12] + "..."
		}
		fmt.Fprintf(&b, "- Staged PHOENIX on this PC: %s (%s, SHA-256 %s). Devices still require ENTER before flashing.\n", d.Staged.Version, d.Staged.File, sha)
	} else {
		b.WriteString("- Staged PHOENIX on this PC: none verified yet. Use Dashboard -> Check firmware, then Send update to a live device.\n")
	}
	if cfg.HomeSSID == "" {
		b.WriteString("- PC presence: home SSID not set (motion/who's-home sensing is idle).\n")
	} else {
		homeCount := 0
		for _, p := range d.Presence.People {
			if p.Home {
				homeCount++
			}
		}
		fmt.Fprintf(&b, "- PC presence: SSID %q, ready=%s, motion=%s, %d named/found people home, %d APs in earshot.\n",
			cfg.HomeSSID, readinessBool(d.Presence.Ready), readinessBool(d.Presence.Motion), homeCount, d.Presence.APs)
		if d.Presence.LastErr != "" {
			fmt.Fprintf(&b, "- PC presence scan problem: %s\n", d.Presence.LastErr)
		}
	}

	b.WriteString("\nSD LIBRARY (device manifests)\n")
	if len(d.Manifests) == 0 {
		b.WriteString("- No device library manifest found yet. PHOENIX 0.12.10+ writes /AEGIS/data/library.json on the card and syncs it, so this section can prove what the card holds.\n")
	}
	for _, m := range d.Manifests {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	fmt.Fprintf(&b, "- Wi-Fi position estimates (no-GPS stretches placed from known networks): %d\n", len(d.Map.EstTrack))

	b.WriteString("\nSAFETY / SECRETS\n")
	b.WriteString("- This report contains no device token, Wi-Fi password, GitHub token, or private key.\n")
	b.WriteString("- Firmware updates remain consent-gated: nothing flashes until ENTER is pressed on the Cardputer.\n")
	b.WriteString("- Location values are estimates. The ± radius is part of the result; a pin without a radius should not be treated as exact.\n")
	return b.String()
}

// scanLibraryManifests summarises any device library.json files
// synced into the library (v1.8.7 SD handoff manifests).
func scanLibraryManifests(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "library.json" || len(out) >= 6 {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var m struct {
			Prog  string `json:"prog"`
			FW    string `json:"fw"`
			Files []struct {
				Name  string `json:"name"`
				Bytes int64  `json:"bytes"`
			} `json:"files"`
		}
		if json.Unmarshal(raw, &m) != nil || m.Prog == "" {
			return nil
		}
		var total int64
		for _, f := range m.Files {
			total += f.Bytes
		}
		day := dayOf(root, path)
		if day == "" {
			day = "?"
		}
		out = append(out, fmt.Sprintf("%s fw %s (synced %s): %d files, %s on card", m.Prog, m.FW, day, len(m.Files), humanBytes(total)))
		return nil
	})
	return out
}

// WriteReadinessReport saves the report under Library/Reports and
// returns its path. Loading map data can take a moment on a large
// library, so the Settings button calls this from a goroutine.
func WriteReadinessReport(core *Core) (string, error) {
	if core == nil || core.Cfg == nil {
		return "", fmt.Errorf("companion core is not ready")
	}
	staged, _, hasStaged := stagedFw(core.Cfg)
	data := readinessData{
		Now:       time.Now(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Cfg:       core.Cfg,
		Health:    core.Health(),
		Map:       LoadMapData(core.Cfg.Library),
		Presence:  presenceSnapshot(),
		Devices:   core.Live.Snapshot(),
		Staged:    staged,
		HasStaged: hasStaged,
	}
	dir := filepath.Join(core.Cfg.Library, "Reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "aegis-readiness-"+data.Now.Format("20060102-150405")+".txt")
	if err := os.WriteFile(path, []byte(buildReadinessReport(data)), 0o644); err != nil {
		return "", err
	}
	core.Log.Infof("field readiness report written: %s", path)
	return path, nil
}
