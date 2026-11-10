package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestValidLatLonEdges(t *testing.T) {
	for _, tc := range []struct {
		lat, lon float64
		want     bool
	}{
		{0, -72.0759, true}, // equator is a real place
		{41.5247, 0, true},  // prime meridian is a real place
		{0, 0, false},       // universal no-fix sentinel
		{math.NaN(), -72, false},
		{41, math.Inf(1), false},
		{91, -72, false},
		{41, -181, false},
	} {
		if got := validLatLon(tc.lat, tc.lon); got != tc.want {
			t.Fatalf("validLatLon(%v,%v)=%v want %v", tc.lat, tc.lon, got, tc.want)
		}
	}
}

func TestEstimateMapPointRejectsStrongestGPSSpike(t *testing.T) {
	trueLat, trueLon := 41.5247, -72.0759
	obs := []MapPoint{
		{BSSID: "AA", SSID: "Home", RSSI: -30, Lat: trueLat + 0.01, Lon: trueLon, AccuracyM: 20, Sightings: 1}, // GPS spike, strongest
		{BSSID: "AA", SSID: "Home", RSSI: -44, Lat: trueLat + 0.0002, Lon: trueLon + 0.0002, AccuracyM: 15, Sightings: 1},
		{BSSID: "AA", SSID: "Home", RSSI: -46, Lat: trueLat - 0.0002, Lon: trueLon + 0.0001, AccuracyM: 15, Sightings: 1},
		{BSSID: "AA", SSID: "Home", RSSI: -48, Lat: trueLat + 0.0001, Lon: trueLon - 0.0002, AccuracyM: 18, Sightings: 1},
		{BSSID: "AA", SSID: "Home", RSSI: -50, Lat: trueLat - 0.0001, Lon: trueLon - 0.0001, AccuracyM: 18, Sightings: 1},
		{BSSID: "AA", SSID: "Home", RSSI: -52, Lat: trueLat, Lon: trueLon, AccuracyM: 20, Sightings: 1},
	}
	got := estimateMapPoint(obs)
	if d := distanceM(got.Lat, got.Lon, trueLat, trueLon); d > 100 {
		t.Fatalf("estimate is %.0f m from cluster, want <=100 m: %+v", d, got)
	}
	if got.RSSI != -30 {
		t.Fatalf("strongest RSSI metadata lost: %+v", got)
	}
	if got.Observations < 4 || got.UncertaintyM <= 0 {
		t.Fatalf("multi-fix metadata wrong: %+v", got)
	}
}

func TestEstimateMapPointSingleFixStaysHonest(t *testing.T) {
	got := estimateMapPoint([]MapPoint{{BSSID: "AA", RSSI: -40, Lat: 41.5, Lon: -72, HDOP: 2.0}})
	if got.UncertaintyM < 9 || got.UncertaintyM > 11 || got.Observations != 1 {
		t.Fatalf("single HDOP fix uncertainty wrong: %+v", got)
	}
}

func TestCleanTrackRejectsIsolatedSpike(t *testing.T) {
	track := [][2]float64{{41.5, -72.0}, {42.5, -72.0}, {41.5001, -72.0}, {41.5002, -72.0}}
	got := cleanTrack(track)
	if len(got) != 3 {
		t.Fatalf("cleanTrack len=%d want 3: %v", len(got), got)
	}
	for _, p := range got {
		if p[0] > 41.51 {
			t.Fatalf("spike survived: %v", got)
		}
	}
}

func TestRefineHomeFromSSID(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "2026-11-10", "PHOENIX", "raw")
	var lines string
	coords := [][2]float64{{41.5247, -72.0759}, {41.5249, -72.0757}, {41.5245, -72.0761}, {41.5248, -72.0760}}
	for i, c := range coords {
		lines += fmt.Sprintf("{\"ssid\":\"Duffwifi\",\"bssid\":\"AA:BB:CC:DD:EE:01\",\"ch\":6,\"rssi\":%d,\"lat\":%.6f,\"lon\":%.6f,\"sats\":10,\"hdop\":1.2}\n", -42-i*3, c[0], c[1])
	}
	writeFixture(t, filepath.Join(raw, "wardrive.jsonl"), lines)
	lat, lon, unc, fixes, err := RefineHomeFromSSID(root, "Duffwifi")
	if err != nil {
		t.Fatal(err)
	}
	if fixes < 3 || unc <= 0 || unc > 500 {
		t.Fatalf("refine metadata wrong: unc=%.1f fixes=%d", unc, fixes)
	}
	if d := distanceM(lat, lon, 41.5247, -72.0759); d > 100 {
		t.Fatalf("refined home %.0f m away", d)
	}
	if _, _, _, _, err := RefineHomeFromSSID(root, "NotHome"); err == nil {
		t.Fatal("unknown SSID refined successfully")
	}
}

func TestGeocodeZipDetailedBoundsAccuracy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"lat":"41.5247","lon":"-72.0759","display_name":"Norwich, Connecticut, 06360, United States","boundingbox":["41.40","41.65","-72.25","-71.90"]}]`)
	}))
	defer srv.Close()
	old := nominatimBase
	nominatimBase = srv.URL
	defer func() { nominatimBase = old }()
	_, _, _, acc, err := geocodeZipDetailed(context.Background(), "06360")
	if err != nil {
		t.Fatal(err)
	}
	if acc < 5000 || acc > 25000 {
		t.Fatalf("ZIP accuracy estimate %.0f m outside expected area-level range", acc)
	}
}
