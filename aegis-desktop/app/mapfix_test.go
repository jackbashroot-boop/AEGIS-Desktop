package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Two GPX days/routes must stay two segments: the route length is
// the sum of the drives, never including the jump between them.
func TestTrackSegmentsAndRoute(t *testing.T) {
	dir := t.TempDir()
	gpx1 := `<?xml version="1.0"?><gpx><trk><trkseg>` +
		`<trkpt lat="41.5200" lon="-72.0700"></trkpt><trkpt lat="41.5210" lon="-72.0700"></trkpt>` +
		`</trkseg></trk></gpx>`
	gpx2 := `<?xml version="1.0"?><gpx><trk><trkseg>` +
		`<trkpt lat="41.6200" lon="-72.1700"></trkpt><trkpt lat="41.6210" lon="-72.1700"></trkpt>` +
		`</trkseg></trk></gpx>`
	if err := os.WriteFile(filepath.Join(dir, "a.gpx"), []byte(gpx1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.gpx"), []byte(gpx2), 0o644); err != nil {
		t.Fatal(err)
	}
	d := LoadMapData(dir)
	if len(d.Track) != 4 {
		t.Fatalf("track points = %d, want 4", len(d.Track))
	}
	if len(d.TrackSeg) != 1 || d.TrackSeg[0] != 2 {
		t.Fatalf("TrackSeg = %v, want [2]", d.TrackSeg)
	}
	// Each drive is ~111 m; glued together it would read ~15 km.
	if d.RouteKm < 0.15 || d.RouteKm > 0.30 {
		t.Fatalf("RouteKm = %.3f, want ~0.22 (segments only)", d.RouteKm)
	}
}

// A record with latitude/longitude swapped (southern ocean instead
// of Connecticut) is repaired when its mirror lands by the cluster.
func TestSwappedCoordRepair(t *testing.T) {
	dir := t.TempDir()
	good := ""
	for i := 0; i < 4; i++ {
		good += `{"ssid":"Home","bssid":"AA:BB:CC:DD:EE:0` + string(rune('0'+i)) + `","rssi":-50,"lat":41.5247,"lon":-72.0759}` + "\n"
	}
	good += `{"ssid":"Swapped","bssid":"AA:BB:CC:DD:EE:FF","rssi":-40,"lat":-72.0759,"lon":41.5247}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "wardrive.jsonl"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	d := LoadMapData(dir)
	found := false
	for _, p := range d.Points {
		if p.BSSID == "AA:BB:CC:DD:EE:FF" {
			found = true
			if p.Lat < 41 || p.Lat > 42 || p.Lon < -73 || p.Lon > -72 {
				t.Fatalf("swapped point not repaired: %.4f, %.4f", p.Lat, p.Lon)
			}
		}
	}
	if !found {
		t.Fatal("swapped BSSID missing from points")
	}
	// And the frame must hug Connecticut, not the whole Atlantic.
	_, minLon, _, maxLon, ok := frameBounds(d)
	if !ok || minLon < -73 || maxLon > -71 {
		t.Fatalf("frame bounds wrong: lon %.3f..%.3f ok=%v", minLon, maxLon, ok)
	}
}

// One far-flung point must not stretch the frame.
func TestFrameBoundsTrimsOutlier(t *testing.T) {
	d := MapData{}
	for i := 0; i < 6; i++ {
		d.Points = append(d.Points, MapPoint{Lat: 41.52 + float64(i)*0.001, Lon: -72.07})
	}
	d.Points = append(d.Points, MapPoint{Lat: 25.79, Lon: -80.32}) // Miami outlier
	minLat, _, maxLat, _, ok := frameBounds(d)
	if !ok {
		t.Fatal("no frame")
	}
	if minLat < 41.4 || maxLat > 41.7 {
		t.Fatalf("frame stretched by outlier: lat %.3f..%.3f", minLat, maxLat)
	}
}
