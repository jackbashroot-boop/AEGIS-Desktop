package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMapDataMergesFormats(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "2026-11-10", "PHOENIX", "raw")

	writeFixture(t, filepath.Join(raw, "wifi_map.geojson"),
		`{"type":"FeatureCollection","features":[`+
			`{"type":"Feature","geometry":{"type":"Point","coordinates":[-72.0759,41.5247]},"properties":{"ssid":"HomeNet","bssid":"AA:BB:CC:DD:EE:01","channels":"6","best_rssi":-52,"sightings":14}},`+
			`{"type":"Feature","geometry":{"type":"Point","coordinates":[-72.0800,41.5300]},"properties":{"ssid":"Cafe","bssid":"AA:BB:CC:DD:EE:02","channels":"1,11","best_rssi":-81,"sightings":3}},`+
			`{"type":"Feature","geometry":{"type":"Point","coordinates":[0,0]},"properties":{"ssid":"NoFix","bssid":"AA:BB:CC:DD:EE:09","best_rssi":-40,"sightings":1}}`+
			`]}`)
	// same BSSID as HomeNet but weaker here -> geojson sighting must win;
	// one record without a fix -> NoGPS; one non-sighting -> ignored.
	writeFixture(t, filepath.Join(raw, "wardrive.jsonl"),
		`{"ts":12,"ssid":"HomeNet","bssid":"AA:BB:CC:DD:EE:01","ch":6,"rssi":-70,"enc":3,"lat":41.6000,"lon":-72.1000,"sats":7}`+"\n"+
			`{"ts":13,"ssid":"Weak","bssid":"AA:BB:CC:DD:EE:03","ch":1,"rssi":-88,"enc":3}`+"\n"+
			`{"kind":"boot","n":1}`+"\n")
	writeFixture(t, filepath.Join(raw, "wifi_map.csv"),
		"ssid,bssid,channel,best_rssi,sightings,lat,lon\n"+
			"ShopNet,AA:BB:CC:DD:EE:04,11,-64,5,41.5310,-72.0770\n")
	writeFixture(t, filepath.Join(raw, "track.gpx"),
		`<?xml version="1.0"?><gpx><trk><trkseg>`+
			`<trkpt lat="41.5247" lon="-72.0759"></trkpt>`+
			`<trkpt lat="41.5347" lon="-72.0759"></trkpt>`+
			`<trkpt lat="41.5347" lon="-72.0759"></trkpt>`+
			`</trkseg></trk></gpx>`)

	d := LoadMapData(root)
	if len(d.Points) != 3 {
		t.Fatalf("points = %d, want 3 (%+v)", len(d.Points), d.Points)
	}
	if d.Points[0].BSSID != "AA:BB:CC:DD:EE:01" || d.Points[0].RSSI != -52 {
		t.Fatalf("strongest point wrong: %+v", d.Points[0])
	}
	if d.Points[0].Lat != 41.5247 || d.Points[0].Channel != 6 || d.Points[0].Sightings != 14 {
		t.Fatalf("winning record fields wrong: %+v", d.Points[0])
	}
	if d.Points[0].Day != "2026-11-10" {
		t.Fatalf("day = %q", d.Points[0].Day)
	}
	if d.NoGPS != 1 {
		t.Fatalf("NoGPS = %d, want 1", d.NoGPS)
	}
	if len(d.Track) != 2 {
		t.Fatalf("track = %d pts, want 2 after dedupe", len(d.Track))
	}
	if d.RouteKm < 1.0 || d.RouteKm > 1.25 {
		t.Fatalf("RouteKm = %.3f, want ~1.11", d.RouteKm)
	}
	if d.Files != 4 {
		t.Fatalf("Files = %d, want 4", d.Files)
	}
}

func TestLoadMapDataWiGLE(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "2026-11-09", "PHOENIX", "raw")
	writeFixture(t, filepath.Join(raw, "wifi_map_wigle.csv"),
		"WigleWifi-1.4,appRelease=AEGIS-PHX,model=CardputerADV\n"+
			"MAC,SSID,AuthMode,FirstSeen,Channel,RSSI,CurrentLatitude,CurrentLongitude,AltitudeMeters,AccuracyMeters,Type\n"+
			"AA:BB:CC:DD:EE:07,WigleNet,WPA2,2026-11-09 10:00:00,6,-58,41.5247,-72.0759,0,0,WIFI\n")
	d := LoadMapData(root)
	if len(d.Points) != 1 || d.Points[0].SSID != "WigleNet" || d.Points[0].RSSI != -58 {
		t.Fatalf("wigle parse wrong: %+v", d.Points)
	}
}

func TestMapHTMLEscapesAndLinks(t *testing.T) {
	d := MapData{
		Points: []MapPoint{{SSID: `"><script>alert(1)</script>`, BSSID: "AA:BB:CC:DD:EE:01",
			Channel: 6, RSSI: -50, Sightings: 2, Lat: 41.5247, Lon: -72.0759, Day: "2026-11-10"}},
		Days: []string{"2026-11-10"},
	}
	h := BuildMapHTML(d)
	if strings.Contains(h, "<script>alert(1)") {
		t.Fatal("SSID not escaped in HTML output")
	}
	if !strings.Contains(h, "google.com/maps/search/?api=1&amp;query=41.524700,-72.075900") &&
		!strings.Contains(h, "google.com/maps/search/?api=1&query=41.524700,-72.075900") {
		t.Fatal("Google Maps link missing")
	}
	if !strings.Contains(h, "leaflet@1.9.4") {
		t.Fatal("leaflet not referenced")
	}
	if u := GoogleMapsURL(41.5247, -72.0759); u != "https://www.google.com/maps/search/?api=1&query=41.524700,-72.075900" {
		t.Fatalf("GoogleMapsURL = %s", u)
	}
}

func TestLoadMapDataEmpty(t *testing.T) {
	d := LoadMapData(t.TempDir())
	if len(d.Points) != 0 || len(d.Track) != 0 {
		t.Fatalf("empty library gave data: %+v", d)
	}
}
