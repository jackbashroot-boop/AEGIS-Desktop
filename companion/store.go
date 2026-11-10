package main

import (
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store owns the dated Library on disk: raw device files, generated
// plain-English reports, and an in-memory summary index for the GUI.
type Store struct {
	root string
	log  *Log

	mu        sync.Mutex
	summaries []DayProg
	sizeBytes int64
	builtAt   time.Time
}

func NewStore(root string, log *Log) *Store {
	return &Store{root: root, log: log}
}

func (s *Store) Root() string { return s.root }

func progAccent(prog string) string {
	switch strings.ToUpper(prog) {
	case "PHOENIX":
		return "#35d3f4"
	case "TALON":
		return "#ffb020"
	case "SCRIBE":
		return "#b388ff"
	case "BENCH":
		return "#2dd4bf"
	case "LANTERN":
		return "#ffd54f"
	case "KEEPER":
		return "#90a4c8"
	case "HEARTH":
		return "#ff8a3d"
	case "HERALD":
		return "#ff7a59"
	}
	return "#9fb4d8"
}

var sevBorder = map[int]string{3: "#ff5252", 2: "#ffb020", 1: "#8a94a8", 0: "#3a4358"}

// Rebuild (re)summarises every day/program in the library and rewrites the
// HTML reports + index. Called after any sync session or import.
func (s *Store) Rebuild() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.MkdirAll(s.root, 0o755)
	dayEntries, _ := os.ReadDir(s.root)
	var days []string
	for _, d := range dayEntries {
		if d.IsDir() && len(d.Name()) == 10 && d.Name()[4] == '-' {
			days = append(days, d.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	var all []DayProg
	var totalSize int64
	for _, day := range days {
		progEntries, _ := os.ReadDir(filepath.Join(s.root, day))
		var progs []string
		for _, p := range progEntries {
			if p.IsDir() {
				if _, err := os.Stat(filepath.Join(s.root, day, p.Name(), "raw")); err == nil {
					progs = append(progs, p.Name())
				}
			}
		}
		sort.Strings(progs)
		for _, prog := range progs {
			rawdir := filepath.Join(s.root, day, prog, "raw")
			dp := summarizeRawDir(day, prog, rawdir)
			for _, f := range dp.Files {
				totalSize += f.Size
			}
			s.writeReport(dp)
			all = append(all, dp)
		}
	}
	_ = s.writeIndex(all)
	s.summaries = all
	s.sizeBytes = totalSize
	s.builtAt = time.Now()
	return len(days)
}

func (s *Store) writeReport(dp DayProg) {
	var cards strings.Builder
	limit := len(dp.Findings)
	if limit > 60 {
		limit = 60
	}
	for _, f := range dp.Findings[:limit] {
		fmt.Fprintf(&cards, `<article class="card" style="border-left:6px solid %s"><h3>%s</h3><p>%s</p><p class="detail">%s</p><details><summary>Details (for finding paths/records)</summary><p>kind: %s · from file: %s</p><pre>%s</pre></details></article>`+"\n",
			sevBorder[f.Sev], html.EscapeString(f.Simple), html.EscapeString(f.Why),
			html.EscapeString(f.Detail), html.EscapeString(f.Kind),
			html.EscapeString(f.Source), html.EscapeString(f.Raw))
	}
	names := make([]string, 0, len(dp.Chains))
	for n := range dp.Chains {
		names = append(names, n)
	}
	sort.Strings(names)
	var chain strings.Builder
	for _, n := range names {
		c := dp.Chains[n]
		fmt.Fprintf(&chain, "<li>%s: %d chained, %d BAD, %d unchained</li>", html.EscapeString(n), c.Chained, c.Bad, c.Unchained)
	}
	if chain.Len() == 0 {
		chain.WriteString("<li>no chained files</li>")
	}
	report := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><title>AEGIS %s %s</title>
<style>body{font-family:system-ui;background:#0b0e14;color:#e8eaf0;max-width:900px;margin:2rem auto;padding:0 1rem}
.card{background:#141a26;border-radius:12px;padding:1rem;margin:1rem 0}.detail{color:#9fb}pre{white-space:pre-wrap;color:#9ab}
.chip{background:#1e2a3d;border-radius:999px;padding:.2rem .7rem;margin-right:.5rem}</style>
<h1><span style="color:%s">●</span> %s — %s</h1>
<p><span class="chip">%d item(s)</span><span class="chip">chain problems: %d</span></p>
<h2>In plain words</h2><p class="card">%s</p>
%s
<h2>Evidence integrity</h2><ul>%s</ul>
<p>Raw files are kept unmodified in the raw/ folder next to this report.</p>
<p style="color:#5b6578">Built by AEGIS Companion %s.</p>`,
		dp.Day, html.EscapeString(dp.Prog), progAccent(dp.Prog), html.EscapeString(dp.Prog), dp.Day,
		len(dp.Findings), dp.Bad, html.EscapeString(dp.Headline), cards.String(), chain.String(), appVersion)
	_ = os.WriteFile(filepath.Join(dp.Dir, "report.html"), []byte(report), 0o644)
}

func (s *Store) writeIndex(all []DayProg) error {
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset='utf-8'><title>AEGIS Library</title>" +
		"<style>body{font-family:system-ui;background:#0b0e14;color:#e8eaf0;max-width:900px;margin:2rem auto;padding:0 1rem}</style>" +
		"<h1>AEGIS Library</h1><p>Newest first. Written plain-English first; raw evidence is kept untouched.</p>")
	curDay := ""
	for _, dp := range all {
		if dp.Day != curDay {
			if curDay != "" {
				b.WriteString("</ul>")
			}
			curDay = dp.Day
			fmt.Fprintf(&b, "<h2>%s</h2><ul>", dp.Day)
		}
		fmt.Fprintf(&b, `<li><a href="%s/%s/report.html">%s</a> — %d item(s), %d chain problem(s). %s</li>`,
			dp.Day, html.EscapeString(dp.Prog), html.EscapeString(dp.Prog), len(dp.Findings), dp.Bad, html.EscapeString(dp.Headline))
	}
	if curDay != "" {
		b.WriteString("</ul>")
	}
	return os.WriteFile(filepath.Join(s.root, "index.html"), []byte(b.String()), 0o644)
}

func (s *Store) Summaries() []DayProg {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DayProg{}, s.summaries...)
}

func (s *Store) SizeBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sizeBytes
}

// TodayStats powers the dashboard stat cards.
func (s *Store) TodayStats() (files, findings, bad int) {
	today := time.Now().Format("2006-01-02")
	for _, dp := range s.Summaries() {
		if dp.Day == today {
			files += len(dp.Files)
			findings += len(dp.Findings)
			bad += dp.Bad
		}
	}
	return
}

// known SD layouts for card-reader import (same set as the Python tool).
var importMap = map[string][]string{
	"PHOENIX": {"AEGIS/data/event_index.jsonl", "AEGIS/logs/wardrive.csv", "AEGIS/data/wifi_map.csv"},
	"TALON":   {"AEGIS_RT/evidence.jsonl", "AEGIS_RT/wardrive.csv"},
	"SCRIBE":  {"AEGIS_SCRIBE/notes.jsonl", "AEGIS_SCRIBE/report.md"},
	"HEARTH":  {"AEGIS_HEARTH/events.jsonl"},
	"HERALD":  {"AEGIS_HERALD/events.jsonl"},
}

// ImportFrom copies the family's known log files from a mounted SD card
// into today's library, then rebuilds reports.
func (s *Store) ImportFrom(sdRoot string) (int, error) {
	if sdRoot == "" {
		return 0, fmt.Errorf("no SD card folder given")
	}
	day := time.Now().Format("2006-01-02")
	n := 0
	for prog, rels := range importMap {
		for _, rel := range rels {
			src := filepath.Join(sdRoot, filepath.FromSlash(rel))
			if _, err := os.Stat(src); err != nil {
				continue
			}
			raw := filepath.Join(s.root, day, prog, "raw")
			if err := os.MkdirAll(raw, 0o755); err != nil {
				return n, err
			}
			if err := copyFile(src, filepath.Join(raw, filepath.Base(src))); err != nil {
				return n, err
			}
			n++
		}
	}
	s.Rebuild()
	return n, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
