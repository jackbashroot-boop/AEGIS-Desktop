package main

import (
	"sort"
	"sync"
	"time"
)

// Live telemetry: Cardputer programs on the home Wi-Fi hold a lightweight
// AEGIS/1 connection open and stream status frames ("what am I doing right
// now") so the desktop can show a real dashboard. TALON is deliberately
// never part of this - the red program stays unlinked, by design.

type HistPt struct {
	At     time.Time
	APs    int
	HasAPs bool
	Batt   int
	HasBat bool
}

type DeviceLive struct {
	Prog      string
	Ver       string
	State     string // scanning / watching / idle / alert / syncing / ...
	Detail    string
	Last      string // last notable event kind, e.g. new_ap
	Batt      int
	HasBatt   bool
	Charging  bool
	APs       int
	HasAPs    bool
	Pct       int // progress % of a long operation, -1 when none
	Events    int
	GPSAcc    int // optional GNSS estimated accuracy, metres (PHOENIX 0.12.7+)
	HasGPSAcc bool
	GPSRej    int // optional count of rejected impossible GPS jumps
	HasGPSRej bool
	FirstSeen time.Time
	LastSeen  time.Time
	Frames    int
	Hist      []HistPt
}

type LiveRegistry struct {
	mu          sync.Mutex
	devs        map[string]*DeviceLive
	OnlineAfter time.Duration // no frame for this long => offline
}

func NewLiveRegistry() *LiveRegistry {
	return &LiveRegistry{devs: map[string]*DeviceLive{}, OnlineAfter: 12 * time.Second}
}

// Frame is one parsed live status line from a device.
type Frame struct {
	Prog     string
	Ver      string
	State    string
	Detail   string
	Last     string
	Batt     int
	HasBatt  bool
	Charging bool
	APs      int
	HasAPs   bool
	Pct      int
	HasPct   bool
	Events   int
	HasEvts  bool
	GPSAcc   int
	HasGPS   bool
	GPSRej   int
	HasGPSR  bool
}

func (r *LiveRegistry) Update(f Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devs[f.Prog]
	now := time.Now()
	if !ok {
		d = &DeviceLive{Prog: f.Prog, FirstSeen: now, Pct: -1}
		r.devs[f.Prog] = d
	}
	if f.Ver != "" {
		d.Ver = f.Ver
	}
	if f.State != "" {
		d.State = f.State
	}
	d.Detail = f.Detail
	if f.Last != "" {
		d.Last = f.Last
	}
	if f.HasBatt {
		d.Batt, d.HasBatt = f.Batt, true
	}
	d.Charging = f.Charging
	if f.HasAPs {
		d.APs, d.HasAPs = f.APs, true
	}
	if f.HasPct {
		d.Pct = f.Pct
	} else {
		d.Pct = -1
	}
	if f.HasEvts {
		d.Events = f.Events
	}
	if f.HasGPS {
		d.GPSAcc, d.HasGPSAcc = f.GPSAcc, true
	}
	if f.HasGPSR {
		d.GPSRej, d.HasGPSRej = f.GPSRej, true
	}
	d.LastSeen = now
	d.Frames++
	// History sampled at most every 2 s, kept ~8 minutes for sparklines.
	if n := len(d.Hist); n == 0 || now.Sub(d.Hist[n-1].At) >= 2*time.Second {
		d.Hist = append(d.Hist, HistPt{At: now, APs: d.APs, HasAPs: d.HasAPs, Batt: d.Batt, HasBat: d.HasBatt})
		if len(d.Hist) > 240 {
			d.Hist = append([]HistPt{}, d.Hist[len(d.Hist)-240:]...)
		}
	}
}

func (r *LiveRegistry) Online(d DeviceLive, now time.Time) bool {
	return now.Sub(d.LastSeen) <= r.OnlineAfter && !d.LastSeen.IsZero()
}

// Snapshot returns copies, freshest first.
func (r *LiveRegistry) Snapshot() []DeviceLive {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]DeviceLive, 0, len(r.devs))
	for _, d := range r.devs {
		cp := *d
		cp.Hist = append([]HistPt{}, d.Hist...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

func (r *LiveRegistry) Get(prog string) (DeviceLive, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devs[prog]
	if !ok {
		return DeviceLive{}, false
	}
	cp := *d
	cp.Hist = append([]HistPt{}, d.Hist...)
	return cp, true
}

func (r *LiveRegistry) OnlineCount() int {
	n := 0
	now := time.Now()
	for _, d := range r.Snapshot() {
		if r.Online(d, now) {
			n++
		}
	}
	return n
}
