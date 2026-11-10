package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Golden-image tests: hash the rendered art bitmaps and the offline
// tile composite against stored goldens (testdata/golden.json). These
// guard against silent visual regressions in the art renderer and the
// tile compositor - the things the fuzzers cannot see. Regenerate the
// goldens after an INTENTIONAL visual change with:
//
//	AEGIS_UPDATE_GOLDEN=1 go test -run TestGolden -v .
//
// and eyeball the art-preview sheet / a map screenshot before
// committing the new goldens - a hash cannot tell pretty from broken,
// it can only tell "different".

type goldenFile struct {
	Art           map[string]string `json:"art"`
	TileComposite string            `json:"tile_composite"`
}

func goldenPath() string { return filepath.Join("testdata", "golden.json") }

func loadGoldens(t *testing.T) goldenFile {
	t.Helper()
	var g goldenFile
	raw, err := os.ReadFile(goldenPath())
	if err != nil {
		t.Fatalf("goldens missing (%v) - generate with AEGIS_UPDATE_GOLDEN=1", err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func hashPix(pix []uint8) string {
	h := sha256.Sum256(pix)
	return hex.EncodeToString(h[:])
}

func TestGoldenArt(t *testing.T) {
	got := map[string]string{}
	for _, p := range append([]string{"COMPANION"}, familyOrder...) {
		img := artImage(p)
		if img.Bounds().Dx() != 144 || img.Bounds().Dy() != 144 {
			t.Fatalf("%s art bitmap %v, want 144x144", p, img.Bounds())
		}
		got[p] = hashPix(img.Pix)
	}
	if os.Getenv("AEGIS_UPDATE_GOLDEN") == "1" {
		g := loadGoldensLenient()
		g.Art = got
		saveGoldens(t, g)
		return
	}
	g := loadGoldens(t)
	for p, want := range g.Art {
		if got[p] != want {
			t.Errorf("%s art changed: got %s want %s (if intentional, regenerate goldens and eyeball art-preview/)", p, got[p], want)
		}
	}
}

func TestGoldenTileComposite(t *testing.T) {
	root := t.TempDir()
	// deterministic pattern tiles: pixel colour derives from tile
	// coords, so any origin/zoom/order slip changes the composite hash
	mkTile := func(z, tx, ty int) {
		img := image.NewNRGBA(image.Rect(0, 0, tilePx, tilePx))
		for y := 0; y < tilePx; y++ {
			for x := 0; x < tilePx; x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(tx), uint8(ty), uint8(x ^ y), 255})
			}
		}
		dir := filepath.Join(root, "maps", "tiles", fmt.Sprint(z), fmt.Sprint(tx))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d.png", ty)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	d := MapData{
		Points: []MapPoint{
			{SSID: "a", BSSID: "AA:1", Lat: 41.5247, Lon: -72.0759, RSSI: -50},
			{SSID: "b", BSSID: "AA:2", Lat: 41.5310, Lon: -72.0770, RSSI: -60},
		},
		Track: [][2]float64{{41.5247, -72.0759}, {41.5310, -72.0770}},
	}
	probe := buildTileSetCached(root, d, 0, OfflineArea{MinZoom: 10, MaxZoom: 14})
	z := probe.Zoom // Err expected (empty cache) but Zoom reports the pick
	minLat, minLon, maxLat, maxLon := mapBounds(d)
	padLat, padLon := 0.0015, 0.0015
	if v := (maxLat - minLat) * 0.12; v > padLat {
		padLat = v
	}
	if v := (maxLon - minLon) * 0.12; v > padLon {
		padLon = v
	}
	x0, y0, x1, y1 := tileRangeFor(minLat-padLat, minLon-padLon, maxLat+padLat, maxLon+padLon, z)
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			mkTile(z, tx, ty)
		}
	}
	ts := buildTileSetCached(root, d, 0, OfflineArea{MinZoom: 10, MaxZoom: 14})
	if ts.Err || ts.Img == nil || ts.Partial {
		t.Fatalf("composite failed: %+v", ts)
	}
	got := hashPix(ts.Img.Pix)
	if os.Getenv("AEGIS_UPDATE_GOLDEN") == "1" {
		g := loadGoldensLenient()
		g.TileComposite = got
		saveGoldens(t, g)
		return
	}
	g := loadGoldens(t)
	if got != g.TileComposite {
		t.Errorf("tile composite changed: got %s want %s (if intentional, regenerate goldens and check a map screenshot)", got, g.TileComposite)
	}
}

func loadGoldensLenient() goldenFile {
	raw, err := os.ReadFile(goldenPath())
	if err != nil {
		return goldenFile{Art: map[string]string{}}
	}
	var g goldenFile
	_ = json.Unmarshal(raw, &g)
	if g.Art == nil {
		g.Art = map[string]string{}
	}
	return g
}

func saveGoldens(t *testing.T, g goldenFile) {
	t.Helper()
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldenPath(), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("goldens updated - EYEBALL the visuals before trusting them")
}
