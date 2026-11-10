package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Plain-English explanations, ported 1:1 from the Python companion so a
// report reads identically whichever desktop program built it: the simple
// story first, technical detail folded underneath.
var explainText = map[string][2]string{
	"deauth_flood":        {"Possible Wi-Fi kick-off attack nearby", "Something transmitted many 'disconnect' frames. That can knock devices off a Wi-Fi network. It can also be a misbehaving gadget, so treat it as 'worth checking', not proof of an attacker."},
	"evil_twin":           {"A look-alike copy of a known network", "An access point appeared with the same name as one you trust but a different hardware address. Connecting to the wrong one can expose traffic."},
	"rogue_ap":            {"Unknown access point", "A Wi-Fi transmitter your baseline has never seen. Often just a neighbor's new router or a phone hotspot."},
	"karma_suspect":       {"Something answering many network names", "A device replied to several different network names, which is how 'karma' traps lure phones into auto-joining."},
	"beacon_spam":         {"Lots of fake network names in the air", "Many made-up Wi-Fi names were broadcast. Usually a prank tool or a malfunctioning device; it clutters the airwaves."},
	"ble_skimmer_suspect": {"Possible card-skimmer-style Bluetooth beacon", "A Bluetooth device looked like the beacons found in some payment skimmers. Only a suspicion - check anything new near card readers."},
	"ble_stalker":         {"Bluetooth tag following pattern", "A tracker-style Bluetooth tag (AirTag-like) kept showing up. If it is not yours and it follows you, consider finding it."},
	"gnss_spoof":          {"GPS signal looks unreliable", "Satellite navigation data jumped or disagreed with itself. Could be spoofing, could be weak sky view indoors."},
	"new_ap":              {"New Wi-Fi network appeared", "A transmitter not in the learned baseline showed up at home. Usually a neighbor, a guest phone hotspot, or a new gadget of yours."},
	"baseline":            {"Baseline learned", "The program memorized the normal networks around you so later changes stand out."},
	"scan":                {"Routine scan completed", "A scheduled look around. Nothing about this alone is good or bad."},
	"boot":                {"Program started", "The device booted and began watching."},
	"note":                {"Your note", "Something you wrote down yourself in SCRIBE."},
}

var defaultExplain = [2]string{"Recorded event", "The device logged this. The raw record is kept below in case you or a technician need exact details."}

func explain(kind string) (string, string) {
	if e, ok := explainText[kind]; ok {
		return e[0], e[1]
	}
	return defaultExplain[0], defaultExplain[1]
}

func severity(kind string) int {
	switch kind {
	case "deauth_flood", "evil_twin", "karma_suspect":
		return 3
	case "gnss_spoof", "ble_skimmer_suspect", "ble_stalker":
		return 2
	case "rogue_ap", "new_ap", "beacon_spam":
		return 1
	}
	return 0
}

// ---- family FNV-1a hash chain verification (same math as the firmware
// and KEEPER's on-device verifier) ----

func fnv1a(h uint32, s string) uint32 {
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 0x01000193
	}
	return h
}

type ChainVerdict struct {
	Chained   int
	Bad       int
	Unchained int
}

// verifyChainLines reproduces the family rule exactly: the stored line's
// body (chain suffix stripped, closing brace restored) hashes cumulatively;
// lines without a chain field count as unchained and reset the running head.
func verifyChainLines(lines []string) ChainVerdict {
	var v ChainVerdict
	h := uint32(0x811C9DC5)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pos := strings.LastIndex(line, `,"chain":"`)
		if pos < 0 {
			v.Unchained++
			h = 0x811C9DC5
			continue
		}
		body := line[:pos] + "}"
		h = fnv1a(h, body)
		v.Chained++
		stored := ""
		if len(line) >= 2 {
			stored = line[pos+10 : len(line)-2]
		}
		if fmt.Sprintf("%08x", h) != stored {
			v.Bad++
		}
	}
	return v
}

// ---- summarising one program-day raw folder ----

type Finding struct {
	Kind   string
	Simple string
	Why    string
	Detail string
	Raw    string
	Source string
	Sev    int
}

type FileMeta struct {
	Name string
	Size int64
	Sha  string // short, for display
}

type DayProg struct {
	Day      string
	Prog     string
	Findings []Finding
	Chains   map[string]ChainVerdict
	Bad      int
	Headline string
	Files    []FileMeta
	Dir      string // .../<day>/<prog>
}

func loadJSONL(path string) []map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var recs []map[string]any
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			raw := ln
			if len(raw) > 200 {
				raw = raw[:200]
			}
			recs = append(recs, map[string]any{"kind": "unparsed", "raw": raw})
			continue
		}
		recs = append(recs, m)
	}
	return recs
}

func summarizeRawDir(day, prog, rawdir string) DayProg {
	dp := DayProg{Day: day, Prog: prog, Chains: map[string]ChainVerdict{}, Dir: filepath.Dir(rawdir)}
	entries, _ := os.ReadDir(rawdir)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var findings []Finding
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(rawdir, e.Name())
		if info, err := e.Info(); err == nil {
			dp.Files = append(dp.Files, FileMeta{Name: e.Name(), Size: info.Size()})
		}
		lower := strings.ToLower(e.Name())
		switch {
		case strings.HasSuffix(lower, ".jsonl"):
			data, _ := os.ReadFile(p)
			lines := strings.Split(string(data), "\n")
			verdict := verifyChainLines(lines)
			dp.Chains[e.Name()] = verdict
			dp.Bad += verdict.Bad
			for _, r := range loadJSONL(p) {
				kind, _ := r["kind"].(string)
				if kind == "" {
					kind = "event"
				}
				title, why := explain(kind)
				sevOverride := -1
				if kind == "diagnostic_check" {
					// Device self-diagnosis: the check name + verdict
					// IS the headline; detail is the explanation.
					chk, _ := r["check"].(string)
					ok, _ := r["ok"].(bool)
					det, _ := r["detail"].(string)
					if ok {
						title = "Self-check OK: " + chk
						sevOverride = 0
					} else {
						title = "Self-check PROBLEM: " + chk
						sevOverride = 2
					}
					why = det
				}
				detail, _ := r["title"].(string)
				if detail == "" {
					if fields, ok := r["fields"].(map[string]any); ok && len(fields) > 0 {
						keys := make([]string, 0, len(fields))
						for k := range fields {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						var parts []string
						for i, k := range keys {
							if i >= 4 {
								break
							}
							parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
						}
						detail = strings.Join(parts, ", ")
					}
				}
				raw, _ := json.Marshal(r)
				rawStr := string(raw)
				if len(rawStr) > 400 {
					rawStr = rawStr[:400]
				}
				sev := severity(kind)
				if sevOverride >= 0 {
					sev = sevOverride
				}
				findings = append(findings, Finding{
					Kind: kind, Simple: title, Why: why, Detail: detail,
					Raw: rawStr, Source: e.Name(), Sev: sev,
				})
			}
		case strings.HasSuffix(lower, ".csv"):
			data, _ := os.ReadFile(p)
			rows := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			preview := rows
			if len(preview) > 3 {
				preview = preview[:3]
			}
			findings = append(findings, Finding{
				Kind: "data-file", Simple: "Data table: " + e.Name(),
				Why:    "A table the devices collected (for example wardriving rows). Kept whole for reference.",
				Detail: fmt.Sprintf("%d rows", max(0, len(rows)-1)),
				Raw:    strings.Join(preview, "\n"), Source: e.Name(),
			})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Sev > findings[j].Sev })
	dp.Findings = findings
	dp.Headline = "All quiet - nothing suspicious stood out."
	for _, f := range findings {
		if f.Sev >= 3 {
			dp.Headline = "Worth a look: a high-priority pattern was recorded."
			break
		}
		if f.Kind == "new_ap" {
			dp.Headline = "New Wi-Fi transmitter(s) appeared - probably a neighbor or guest, listed below."
		}
	}
	return dp
}
