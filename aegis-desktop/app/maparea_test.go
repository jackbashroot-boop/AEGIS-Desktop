package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestAreaPlanNorwich(t *testing.T) {
	total, perZoom := AreaTilePlan(41.5247, -72.0759)
	if total < 6000 || total > areaMaxTiles {
		t.Fatalf("Norwich 100x100mi plan = %d tiles, want 6000..%d", total, areaMaxTiles)
	}
	prev := 0
	for z := areaMinZoom; z <= areaMaxZoom; z++ {
		if perZoom[z] <= prev {
			t.Fatalf("zoom %d count %d not growing (prev %d)", z, perZoom[z], prev)
		}
		prev = perZoom[z]
	}
}

func TestAreaCovers(t *testing.T) {
	a := OfflineArea{HomeLat: 41.5247, HomeLon: -72.0759, HalfMiles: areaHalfMiles,
		MinZoom: areaMinZoom, MaxZoom: areaMaxZoom, Tiles: 1}
	if !a.Covers(41.50, -72.10, 41.55, -72.05) {
		t.Fatal("drive near home should be covered")
	}
	if a.Covers(40.71, -74.01, 40.72, -74.00) {
		t.Fatal("NYC must be outside a Norwich-centred 100x100mi square")
	}
	if a.Covers(41.52, -72.07, 43.50, -72.07) {
		t.Fatal("drive poking past the north edge must not count as covered")
	}
}

func TestOfflineAreaReceipt(t *testing.T) {
	root := t.TempDir()
	if _, ok := LoadOfflineArea(root); ok {
		t.Fatal("no receipt should exist yet")
	}
	a := OfflineArea{HomeLat: 41.5, HomeLon: -72.1, HalfMiles: 50, MinZoom: 10, MaxZoom: 14, Tiles: 42}
	if err := saveOfflineArea(root, a); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadOfflineArea(root)
	if !ok || got.Tiles != 42 || got.HomeLat != 41.5 {
		t.Fatalf("receipt round-trip: %+v ok=%v", got, ok)
	}
	if err := DeleteOfflineArea(root); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadOfflineArea(root); ok {
		t.Fatal("receipt should be gone after delete")
	}
	if err := DeleteOfflineArea(root); err != nil {
		t.Fatal("deleting twice must not error:", err)
	}
}

func TestBuildTileSetCachedNoNetwork(t *testing.T) {
	root := t.TempDir()
	d := MapData{
		Points: []MapPoint{
			{SSID: "a", BSSID: "AA:1", Lat: 41.5247, Lon: -72.0759, RSSI: -50},
			{SSID: "b", BSSID: "AA:2", Lat: 41.5310, Lon: -72.0770, RSSI: -60},
		},
		Track: [][2]float64{{41.5247, -72.0759}, {41.5310, -72.0770}},
	}
	area := OfflineArea{HomeLat: 41.5247, HomeLon: -72.0759, HalfMiles: areaHalfMiles,
		MinZoom: areaMinZoom, MaxZoom: areaMaxZoom}

	ts := buildTileSetCached(root, d, 0, area)
	if !ts.Err {
		t.Fatalf("empty cache must Err (caller falls back to schematic), got %+v", ts)
	}
	z := ts.Zoom

	// seed every tile the builder will ask for at that zoom
	minLat, minLon, maxLat, maxLon := mapBounds(d)
	padLat := (maxLat - minLat) * 0.12
	padLon := (maxLon - minLon) * 0.12
	if padLat < 0.0015 {
		padLat = 0.0015
	}
	if padLon < 0.0015 {
		padLon = 0.0015
	}
	x0, y0, x1, y1 := tileRangeFor(minLat-padLat, minLon-padLon, maxLat+padLat, maxLon+padLon, z)
	img := image.NewNRGBA(image.Rect(0, 0, tilePx, tilePx))
	for y := 0; y < tilePx; y++ {
		for x := 0; x < tilePx; x++ {
			img.Set(x, y, color.NRGBA{40, 60, 40, 255})
		}
	}
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			dir := filepath.Join(root, "maps", "tiles", fmt.Sprint(z), fmt.Sprint(tx))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprint(ty)+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
		}
	}

	ts = buildTileSetCached(root, d, 0, area)
	if ts.Err || ts.Img == nil {
		t.Fatalf("seeded cache must composite, got Err=%v", ts.Err)
	}
	if ts.Partial {
		t.Fatal("fully seeded range must not be Partial")
	}
	if _, ok := loadCachedTile(root, z, x0, y0); !ok {
		t.Fatal("loadCachedTile should find a seeded tile")
	}
	if _, ok := loadCachedTile(root, z, x0+500, y0); ok {
		t.Fatal("loadCachedTile must miss unseeded tiles")
	}
}
