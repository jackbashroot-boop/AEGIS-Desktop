package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// v1.8.7: Wi-Fi positioning from no-GPS sightings + mesh sharing
// exports.

func TestEstimateWiFiPositions(t *testing.T) {
	known := map[string]MapPoint{
		"AA:BB:CC:DD:EE:01": {BSSID: "AA:BB:CC:DD:EE:01", Lat: 41.0000, Lon: -73.0000, UncertaintyM: 20},
		"AA:BB:CC:DD:EE:02": {BSSID: "AA:BB:CC:DD:EE:02", Lat: 41.0009, Lon: -73.0000, UncertaintyM: 20},
		"AA:BB:CC:DD:EE:03": {BSSID: "AA:BB:CC:DD:EE:03", Lat: 41.0000, Lon: -73.0009, UncertaintyM: 20},
	}
	orphans := []wifiSighting{
		{BSSID: "AA:BB:CC:DD:EE:01", RSSI: -50, TS: 100},
		{BSSID: "AA:BB:CC:DD:EE:02", RSSI: -55, TS: 101},
		{BSSID: "AA:BB:CC:DD:EE:03", RSSI: -60, TS: 102},
		{BSSID: "FF:FF:FF:FF:FF:FF", RSSI: -40, TS: 103}, // unknown AP: ignored
	}
	track, unc := estimateWiFiPositions(known, orphans)
	if len(track) != 1 || len(unc) != 1 {
		t.Fatalf("estimates = %d points, want 1", len(track))
	}
	lat, lon := track[0][0], track[0][1]
	if lat < 40.9995 || lat > 41.0010 || lon < -73.0010 || lon > -72.9995 {
		t.Errorf("estimate (%f,%f) not inside the AP triangle", lat, lon)
	}
	if unc[0] < 25 || unc[0] > 900 {
		t.Errorf("uncertainty %.0f outside honest clamps", unc[0])
	}
	// One known AP is never enough.
	if tr, _ := estimateWiFiPositions(known, orphans[:1]); len(tr) != 0 {
		t.Errorf("single-AP group produced %d estimates, want 0", len(tr))
	}
	// Nothing known: nothing estimated.
	if tr, _ := estimateWiFiPositions(nil, orphans); len(tr) != 0 {
		t.Errorf("empty atlas produced estimates")
	}
}

func TestShareExports(t *testing.T) {
	d := MapData{
		Points: []MapPoint{
			{SSID: "HomeNet", BSSID: "AA:BB:CC:DD:EE:01", Channel: 6, RSSI: -52, Sightings: 4, Lat: 41.1, Lon: -73.2, UncertaintyM: 35, Observations: 3},
			{SSID: "Cafe <WiFi>", BSSID: "AA:BB:CC:DD:EE:02", Channel: 11, RSSI: -70, Sightings: 1, Lat: 41.2, Lon: -73.3, UncertaintyM: 80, Observations: 1},
		},
		Track:    [][2]float64{{41.1, -73.2}, {41.2, -73.3}},
		EstTrack: [][2]float64{{41.15, -73.25}, {41.16, -73.26}},
	}
	gpx := BuildGPXWaypoints(d)
	if strings.Count(gpx, "<wpt ") != 2 {
		t.Errorf("gpx waypoints = %d, want 2:\n%s", strings.Count(gpx, "<wpt "), gpx)
	}
	if !strings.Contains(gpx, "Cafe &lt;WiFi&gt;") {
		t.Errorf("gpx SSID not XML-escaped")
	}
	gj, err := BuildGeoJSON(d)
	if err != nil {
		t.Fatal(err)
	}
	var fc struct {
		Type     string `json:"type"`
		Features []any  `json:"features"`
	}
	if err := json.Unmarshal(gj, &fc); err != nil {
		t.Fatalf("geojson invalid: %v", err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 4 {
		t.Errorf("geojson features = %d, want 4 (2 points + GPS + est lines)", len(fc.Features))
	}
	csv := BuildAPCSV(d)
	if !strings.HasPrefix(csv, "BSSID,SSID,") || strings.Count(csv, "\n") != 3 {
		t.Errorf("csv shape wrong:\n%s", csv)
	}
	dir := t.TempDir()
	out, err := WriteShareExports(dir, d)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("WriteShareExports returned empty dir")
	}
}
