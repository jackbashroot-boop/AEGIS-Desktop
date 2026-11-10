package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Link health: the companion checks itself and the link so the user
// never has to wonder whether the family is connected. Everything
// here is computed from state the program already has; the writable
// probe is cached because Health() is called on every UI frame.

type HealthItem struct {
	Label  string
	Detail string
	OK     bool
	Warn   bool // shown amber, not red: "worth a look", not broken
}

const healthProbeInterval = 15 * time.Second

// libraryWritable probes the library once per interval.
func (c *Core) libraryWritable() bool {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if time.Since(c.lastProbe) < healthProbeInterval && c.probed {
		return c.probeOK
	}
	c.probed, c.lastProbe = true, time.Now()
	p := filepath.Join(c.Cfg.Library, ".aegis-probe")
	if err := os.WriteFile(p, []byte("probe"), 0o644); err != nil {
		c.probeOK = false
	} else {
		c.probeOK = os.Remove(p) == nil
	}
	return c.probeOK
}

// selfCheck logs the startup checks once, plainly.
func (c *Core) selfCheck() {
	c.Log.Infof("self-check: config %s", configPath())
	if c.libraryWritable() {
		c.Log.Infof("self-check: library is writable (%s)", c.Cfg.Library)
	} else {
		c.Log.Errorf("self-check: LIBRARY NOT WRITABLE (%s) - syncs cannot be filed!", c.Cfg.Library)
	}
	if len(c.Cfg.Token) >= 16 {
		c.Log.Infof("self-check: device token present (...%s)", c.Cfg.Token[len(c.Cfg.Token)-4:])
	} else {
		c.Log.Errorf("self-check: device token missing/short - devices cannot authenticate")
	}
	if port, ok := c.Server.Listening(); ok {
		c.Log.Infof("self-check: device listener ok on :%d", port)
	} else {
		c.Log.Errorf("self-check: device listener is DOWN - check the port in Settings")
	}
	if port, ok := c.Disco.Running(); ok {
		c.Log.Infof("self-check: discovery responder ok on udp :%d", port)
	} else {
		c.Log.Warnf("self-check: discovery responder down - devices must use the exact address in desktop.txt")
	}
}

// Health computes the dashboard's link-health list.
func (c *Core) Health() []HealthItem {
	var items []HealthItem
	if port, ok := c.Server.Listening(); ok {
		items = append(items, HealthItem{"Listener", fmt.Sprintf("accepting devices on :%d", port), true, false})
	} else {
		items = append(items, HealthItem{"Listener", "DOWN - devices cannot connect; check Settings", false, false})
	}
	if port, ok := c.Disco.Running(); ok {
		seek := ""
		if s := c.Disco.Seekers(); len(s) > 0 {
			latest := time.Time{}
			for _, t := range s {
				if t.After(latest) {
					latest = t
				}
			}
			seek = fmt.Sprintf(", last search %s ago", ago(latest))
		}
		items = append(items, HealthItem{"Auto-discovery", fmt.Sprintf("answering on udp :%d%s", port, seek), true, false})
	} else {
		items = append(items, HealthItem{"Auto-discovery", "off - devices must use the exact desktop.txt address", false, true})
	}
	if c.libraryWritable() {
		items = append(items, HealthItem{"Library", "writable, " + humanBytes(c.Store.SizeBytes()) + " filed", true, false})
	} else {
		items = append(items, HealthItem{"Library", "NOT writable - syncs cannot be filed", false, false})
	}

	// Chain health across everything filed today + device freshness
	// from the most recent report day per program.
	sums := c.Store.Summaries()
	badProgs := map[string]bool{}
	lastDay := map[string]string{}
	for _, s := range sums {
		if s.Bad > 0 {
			badProgs[s.Prog] = true
		}
		if s.Day > lastDay[s.Prog] {
			lastDay[s.Prog] = s.Day
		}
	}
	if len(badProgs) == 0 {
		items = append(items, HealthItem{"Hash chains", "every filed log verifies", true, false})
	} else {
		names := make([]string, 0, len(badProgs))
		for p := range badProgs {
			names = append(names, p)
		}
		sort.Strings(names)
		items = append(items, HealthItem{"Hash chains", fmt.Sprintf("PROBLEM in %v - see Reports", names), false, false})
	}

	online := map[string]bool{}
	for _, d := range c.Live.Snapshot() {
		if c.Live.Online(d, time.Now()) {
			online[d.Prog] = true
		}
	}
	if len(online) > 0 {
		names := make([]string, 0, len(online))
		for p := range online {
			names = append(names, p)
		}
		sort.Strings(names)
		items = append(items, HealthItem{"Live link", fmt.Sprintf("%v streaming now", names), true, false})
	}
	today := time.Now().Format("2006-01-02")
	progs := make([]string, 0, len(lastDay))
	for p := range lastDay {
		progs = append(progs, p)
	}
	sort.Strings(progs)
	for _, p := range progs {
		if online[p] {
			continue
		}
		if lastDay[p] == today {
			items = append(items, HealthItem{p, "filed reports today; not streaming right now", true, false})
		} else {
			d, _ := time.Parse("2006-01-02", lastDay[p])
			days := int(time.Since(d).Hours() / 24)
			items = append(items, HealthItem{p, fmt.Sprintf("last filed %s (%dd ago) - normal if the device is off", lastDay[p], days), true, days > 3})
		}
	}
	return items
}

// linkWatchdog notices live devices going quiet (or returning) so
// the Activity Log tells the story without the user watching it.
func (c *Core) linkWatchdog(stop <-chan struct{}) {
	prev := map[string]bool{}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		now := map[string]bool{}
		for _, d := range c.Live.Snapshot() {
			if c.Live.Online(d, time.Now()) {
				now[d.Prog] = true
			}
		}
		for p := range prev {
			if !now[p] {
				c.Log.Warnf("link: %s went quiet (no live frames for a while)", p)
			}
		}
		for p := range now {
			if !prev[p] && len(prev) > 0 {
				c.Log.Infof("link: %s is streaming again", p)
			}
		}
		prev = now
	}
}
