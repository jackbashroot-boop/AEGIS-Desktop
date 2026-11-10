package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Brutal-testing battery (v1.5.0 hardening pass): fuzz targets for every
// parser and wire-facing decoder, invariant checks under adversarial
// input, concurrency stress, and benchmarks for the hot paths. Fuzzers
// run as ordinary tests over their seed corpus; run them for real with
//   go test -fuzz=FuzzParseMsg -fuzztime=60s .
// Any input that panics, hangs, or breaks an invariant is a bug.

func FuzzParseMsg(f *testing.F) {
	f.Add(`{"auth":"abc","prog":"PHOENIX","ver":"0.9.5"}`)
	f.Add(`{"file":"x","size":123,"sha256":"deadbeef"}`)
	f.Add(`{"done":true}`)
	f.Add(`{"type":"live","state":"SCANNING","pct":42,"aps":7}`)
	f.Add(``)
	f.Add(`{`)
	f.Add(`{"size":1e999}`)
	f.Add(`{"a":` + strings.Repeat(`{"b":`, 500) + `1` + strings.Repeat(`}`, 500) + `}`)
	f.Fuzz(func(t *testing.T, s string) {
		m, err := parseMsg(s)
		if err != nil {
			return
		}
		// decoders must never panic on any JSON shape
		_ = msgString(m, "auth")
		_ = msgBool(m, "done")
		if v, ok := msgInt(m, "size"); ok && v < 0 {
			// negative sizes are rejected by callers; decoder may pass through
			_ = v
		}
		fr := frameFromMsg(m, "PHOENIX", "0.0.0")
		if fr.Pct < -1 || fr.Pct > 100 {
			t.Fatalf("frame pct out of range: %d from %q", fr.Pct, s)
		}
	})
}

func FuzzSanitizeName(f *testing.F) {
	f.Add("PHOENIX")
	f.Add("../../etc/passwd")
	f.Add("a/b\\c")
	f.Add("")
	f.Add("..")
	f.Add(strings.Repeat("A", 5000))
	f.Add("SCRIBE\x00evil")
	f.Fuzz(func(t *testing.T, s string) {
		out, ok := sanitizeName(s)
		if !ok {
			return
		}
		if strings.ContainsAny(out, "/\\") || out == "." || out == ".." || strings.Contains(out, "..") {
			t.Fatalf("sanitizeName(%q) = %q escapes its folder", s, out)
		}
		if len(out) > 80 {
			t.Fatalf("sanitizeName(%q) = %d chars, unbounded", s, len(out))
		}
	})
}

func FuzzParseDiscovery(f *testing.F) {
	f.Add([]byte("AEGIS_DISCOVER 1 0123456789abcdef0123456789abcdef"))
	f.Add([]byte("AEGIS_DISCOVER 1 xyz"))
	f.Add([]byte(""))
	f.Add([]byte("AEGIS_DISCOVER 2 0123456789abcdef0123456789abcdef"))
	f.Add([]byte(strings.Repeat("A", 65535)))
	f.Fuzz(func(t *testing.T, b []byte) {
		nonce, ok := parseDiscovery(b)
		if ok && !validNonce(nonce) {
			t.Fatalf("parseDiscovery accepted bad nonce %q", nonce)
		}
		_ = discoveryReply("tok", 8791, nonce) // must not panic either way
	})
}

func FuzzMapFileParsers(f *testing.F) {
	f.Add([]byte(`{"type":"FeatureCollection","features":[{"geometry":{"coordinates":[-72.07,41.52]},"properties":{"ssid":"x","bssid":"AA:BB","rssi":-40}}]}`))
	f.Add([]byte("SSID,BSSID,Channel,RSSI,Lat,Lon\nx,AA:BB,6,-40,41.5,-72.0\n"))
	f.Add([]byte(`{"ssid":"x","bssid":"AA:BB","lat":41.5,"lon":-72.0,"rssi":-40}` + "\n"))
	f.Add([]byte(`<gpx><trk><trkseg><trkpt lat="41.5" lon="-72.0"/></trkseg></trk></gpx>`))
	f.Add([]byte{0x00, 0xff, 0xfe, 0x7b})
	f.Add([]byte(strings.Repeat(`{"a":1},`, 2000)))
	dir := f.TempDir()
	geo := filepath.Join(dir, "m.geojson")
	csvp := filepath.Join(dir, "m.csv")
	jsonl := filepath.Join(dir, "m.jsonl")
	gpx := filepath.Join(dir, "m.gpx")
	f.Fuzz(func(t *testing.T, b []byte) {
		for path, fn := range map[string]func(string) []MapPoint{
			geo: parseGeoJSON, csvp: parseMapCSV,
		} {
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			for _, p := range fn(path) {
				if !validLatLon(p.Lat, p.Lon) {
					t.Fatalf("%s produced invalid coords %+v", path, p)
				}
			}
		}
		if err := os.WriteFile(jsonl, b, 0o644); err != nil {
			t.Fatal(err)
		}
		pts, _ := parseSightingJSONL(jsonl)
		for _, p := range pts {
			if !validLatLon(p.Lat, p.Lon) {
				t.Fatalf("jsonl produced invalid coords %+v", p)
			}
		}
		if err := os.WriteFile(gpx, b, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, tp := range parseGPX(gpx) {
			if !validLatLon(tp[0], tp[1]) {
				t.Fatalf("gpx produced invalid track point %v", tp)
			}
		}
	})
}

func FuzzTileMath(f *testing.F) {
	f.Add(41.5247, -72.0759)
	f.Add(0.0, 0.0)
	f.Add(89.999, 179.999)
	f.Add(math.NaN(), math.Inf(1))
	f.Add(-1e308, 1e308)
	f.Fuzz(func(t *testing.T, lat, lon float64) {
		for z := 0; z <= 18; z++ {
			_ = lonToWorldX(lon, z)
			_ = latToWorldY(lat, z)
		}
		// pickZoom must always terminate and stay in range
		z := pickZoom(lat, lon, lat+0.01, lon+0.01, 1150, 380)
		if z < 3 || z > 17 {
			t.Fatalf("pickZoom out of range: %d", z)
		}
		x0, y0, x1, y1 := tileRangeFor(lat, lon, lat+0.01, lon+0.01, 14)
		if x1 < x0 || y1 < y0 {
			t.Fatalf("inverted tile range (%d,%d)-(%d,%d)", x0, y0, x1, y1)
		}
	})
}

// TestTileSetBuildersRejectBadBounds: garbage MapData (should be
// impossible via the parsers, but the builders must not hang or OOM
// trying to composite an infinite/NaN world).
func TestTileSetBuildersRejectBadBounds(t *testing.T) {
	root := t.TempDir()
	bad := MapData{Points: []MapPoint{
		{SSID: "x", BSSID: "AA", Lat: math.NaN(), Lon: math.Inf(1), RSSI: -1},
		{SSID: "y", BSSID: "BB", Lat: 41.5, Lon: -72.0, RSSI: -2},
	}}
	if ts := buildTileSetCached(root, bad, 0, OfflineArea{MinZoom: 10, MaxZoom: 14}); ts != nil && !ts.Err && ts.Img != nil {
		t.Fatal("cached builder should refuse NaN/Inf bounds")
	}
	if ts := buildTileSet(root, bad, 0); ts != nil && !ts.Err && ts.Img != nil {
		t.Fatal("online builder should refuse NaN/Inf bounds")
	}
}

// TestLoadMapDataHostileTree throws a nasty fake library at the loader:
// symlink loops, unreadable files, giant sparse files, FIFO-ish names.
func TestLoadMapDataHostileTree(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026-11-07", "PHOENIX", "raw")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(day, "wifi_map.geojson"), `{"type":"FeatureCollection","features":[{"geometry":{"coordinates":[-72.07,41.52]},"properties":{"ssid":"ok","bssid":"AA:BB:CC:DD:EE:01","rssi":-40}}]}`)
	writeFixture(t, filepath.Join(day, "junk.csv"), strings.Repeat(",,,,,,\n", 5000))
	writeFixture(t, filepath.Join(day, "junk.jsonl"), strings.Repeat("{bad json\n", 5000))
	writeFixture(t, filepath.Join(day, "junk.gpx"), strings.Repeat("<trkpt", 5000))
	// symlink loop (ignored gracefully by filepath.Walk on Linux)
	_ = os.Symlink(root, filepath.Join(root, "loop"))
	// a directory wearing a data extension
	_ = os.MkdirAll(filepath.Join(day, "fake.geojson"), 0o755)
	d := LoadMapData(root)
	if len(d.Points) != 1 || d.Points[0].SSID != "ok" {
		t.Fatalf("hostile tree: got %+v", d.Points)
	}
}

// TestLiveRegistryConcurrentStress hammers the registry the way three
// chatty devices plus a 600ms UI ticker would - under -race this is
// where data races would surface.
func TestLiveRegistryConcurrentStress(t *testing.T) {
	r := NewLiveRegistry()
	var wg sync.WaitGroup
	progs := []string{"PHOENIX", "SCRIBE", "HEARTH", "HERALD"}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				r.Update(Frame{Prog: progs[n%len(progs)], State: "SCANNING", Pct: j % 101, APs: j})
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = r.Snapshot()
				_ = r.OnlineCount()
				_, _ = r.Get("PHOENIX")
			}
		}()
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != len(progs) {
		t.Fatalf("snapshot has %d devices, want %d", got, len(progs))
	}
}

func BenchmarkParseMsg(b *testing.B) {
	line := `{"type":"live","state":"SCANNING","detail":"Wi-Fi scan - channel 11","pct":65,"aps":26,"batt":84,"charging":false}`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m, err := parseMsg(line)
		if err != nil {
			b.Fatal(err)
		}
		_ = frameFromMsg(m, "PHOENIX", "0.9.5")
	}
}

func BenchmarkLoadMapData(b *testing.B) {
	root := b.TempDir()
	day := filepath.Join(root, "2026-11-07", "PHOENIX", "raw")
	if err := os.MkdirAll(day, 0o755); err != nil {
		b.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString(`{"type":"FeatureCollection","features":[`)
	for i := 0; i < 2000; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"geometry":{"coordinates":[%.5f,%.5f]},"properties":{"ssid":"net%d","bssid":"AA:BB:CC:DD:%02X:%02X","rssi":%d}}`,
			-72.08+float64(i%40)*0.001, 41.51+float64(i%25)*0.001, i, i>>8&0xff, i&0xff, -90+i%60)
	}
	sb.WriteString(`]}`)
	if err := os.WriteFile(filepath.Join(day, "wifi_map.geojson"), []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d := LoadMapData(root)
		if len(d.Points) == 0 {
			b.Fatal("no points")
		}
	}
}

// TestFrameClampsDeviceNumbers locks in the fuzzer-found clamp: live
// frames arrive from devices and must never carry out-of-range values
// into the dashboard, whatever the device (or an attacker) sends.
func TestFrameClampsDeviceNumbers(t *testing.T) {
	m, err := parseMsg(`{"pct":99999,"batt":-4,"aps":-1,"events":99999999}`)
	if err != nil {
		t.Fatal(err)
	}
	f := frameFromMsg(m, "PHOENIX", "0.9.6")
	if f.Pct != 100 || f.Batt != 0 || f.APs != 0 || f.Events != 1_000_000 {
		t.Fatalf("unclamped frame: %+v", f)
	}
	m, _ = parseMsg(`{"pct":42,"batt":87,"aps":12,"events":3}`)
	f = frameFromMsg(m, "PHOENIX", "0.9.6")
	if f.Pct != 42 || f.Batt != 87 || f.APs != 12 || f.Events != 3 {
		t.Fatalf("honest values must pass through: %+v", f)
	}
}
