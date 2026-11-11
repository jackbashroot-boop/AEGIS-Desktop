package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// Sharing wardrive results with the LoRa mesh ecosystems
// (v1.8.7). Honest scope: Meshtastic and MeshCore speak their own
// encrypted LoRa protocols with their own sync words, so an AEGIS
// beacon is not receivable by them and theirs is not by us - no
// fake "interop" claims. What DOES travel between ecosystems is
// files: GPX waypoints, GeoJSON and CSV open in the Meshtastic
// app, MeshCore companion tooling, and every mainstream map app.
// These exports are that bridge: your own wardrive data, in the
// formats the whole mesh world already reads.

// BuildGPXWaypoints renders every mapped network as a GPX
// waypoint (name = SSID, description carries BSSID/signal/±).
func BuildGPXWaypoints(d MapData) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="AEGIS Companion" xmlns="http://www.topografix.com/GPX/1/1">` + "\n")
	b.WriteString("<metadata><name>AEGIS wardrive networks</name></metadata>\n")
	for _, p := range d.Points {
		name := p.SSID
		if name == "" {
			name = "(hidden)"
		}
		fmt.Fprintf(&b, "<wpt lat=\"%.6f\" lon=\"%.6f\"><name>%s</name><desc>%s · ch %d · %d dBm · seen %dx · est ±%.0f m from %d fix(es) · AEGIS wardrive</desc></wpt>\n",
			p.Lat, p.Lon, html.EscapeString(name), html.EscapeString(p.BSSID),
			p.Channel, p.RSSI, p.Sightings, p.UncertaintyM, p.Observations)
	}
	b.WriteString("</gpx>\n")
	return b.String()
}

type geoJSONFeature struct {
	Type       string         `json:"type"`
	Geometry   map[string]any `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geoJSONCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

// BuildGeoJSON renders mapped networks (points), the GPS route
// (LineString) and Wi-Fi position estimates (separate LineString,
// clearly labelled estimated) as one FeatureCollection.
func BuildGeoJSON(d MapData) ([]byte, error) {
	fc := geoJSONCollection{Type: "FeatureCollection"}
	for _, p := range d.Points {
		fc.Features = append(fc.Features, geoJSONFeature{
			Type:     "Feature",
			Geometry: map[string]any{"type": "Point", "coordinates": []float64{p.Lon, p.Lat}},
			Properties: map[string]any{
				"ssid": p.SSID, "bssid": p.BSSID, "channel": p.Channel, "rssi": p.RSSI,
				"sightings": p.Sightings, "uncertainty_m": p.UncertaintyM,
				"fixes": p.Observations, "source": "AEGIS wardrive (GPS)",
			},
		})
	}
	coords := func(track [][2]float64) [][]float64 {
		out := make([][]float64, 0, len(track))
		for _, tp := range track {
			out = append(out, []float64{tp[1], tp[0]})
		}
		return out
	}
	if len(d.Track) > 1 {
		fc.Features = append(fc.Features, geoJSONFeature{
			Type:       "Feature",
			Geometry:   map[string]any{"type": "LineString", "coordinates": coords(d.Track)},
			Properties: map[string]any{"name": "AEGIS wardrive route (GPS)", "source": "AEGIS wardrive (GPS)"},
		})
	}
	if len(d.EstTrack) > 1 {
		fc.Features = append(fc.Features, geoJSONFeature{
			Type:       "Feature",
			Geometry:   map[string]any{"type": "LineString", "coordinates": coords(d.EstTrack)},
			Properties: map[string]any{"name": "AEGIS route estimate (Wi-Fi positioning, no GPS)", "source": "AEGIS Wi-Fi estimate"},
		})
	}
	return json.MarshalIndent(fc, "", " ")
}

// BuildAPCSV is the flat spreadsheet form (also WiGLE-friendly
// shaped): one row per mapped network.
func BuildAPCSV(d MapData) string {
	var b strings.Builder
	b.WriteString("BSSID,SSID,Channel,RSSI,Sightings,Latitude,Longitude,UncertaintyM,Fixes,Day\n")
	for _, p := range d.Points {
		fmt.Fprintf(&b, "%s,%q,%d,%d,%d,%.6f,%.6f,%.0f,%d,%s\n",
			p.BSSID, p.SSID, p.Channel, p.RSSI, p.Sightings, p.Lat, p.Lon, p.UncertaintyM, p.Observations, p.Day)
	}
	return b.String()
}

const shareReadme = `AEGIS wardrive - sharing with Meshtastic / MeshCore
====================================================

Files in this folder (regenerate any time from the Wardrive Map page):

- wardrive-waypoints.gpx : every mapped network as a waypoint.
  Opens in the Meshtastic app map, MeshCore companion tools, and
  any GPX app (OsmAnd, Gaia GPS, Google Earth...).
- wardrive.geojson       : networks + GPS route + Wi-Fi-estimated
  route. Opens in geojson.io, QGIS, uMap, and most mesh map tools.
- wardrive-aps.csv       : flat table for spreadsheets / WiGLE.

Honest interop note: Meshtastic and MeshCore use their own
encrypted LoRa protocols and sync words. AEGIS LoRa beacons are
AEGIS-family only, and AEGIS cannot decode Meshtastic/MeshCore
traffic - files are the bridge between the ecosystems today.
Everything here is your own recorded data; share responsibly and
only where you are allowed to map.
`

// WriteShareExports writes the mesh-sharing bundle into
// Library/exports and returns the folder.
func WriteShareExports(root string, d MapData) (string, error) {
	dir := filepath.Join(root, "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	gj, err := BuildGeoJSON(d)
	if err != nil {
		return "", err
	}
	files := map[string][]byte{
		"wardrive-waypoints.gpx": []byte(BuildGPXWaypoints(d)),
		"wardrive.geojson":       gj,
		"wardrive-aps.csv":       []byte(BuildAPCSV(d)),
		"README-share.txt":       []byte(shareReadme),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}
