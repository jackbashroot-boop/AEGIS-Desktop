package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// Interactive wardrive map, written as one self-contained HTML file the
// companion opens in the user's browser. Tiles come from OpenStreet
// Map / CARTO at view time (nothing is uploaded anywhere); every pin
// also carries an "Open in Google Maps" link, so a network can be
// jumped straight into Google Maps / Street View with no API key.
// If the PC is offline the map tiles stay blank but the full sighting
// table underneath still works.

type mapJSONPoint struct {
	SSID      string  `json:"ssid"`
	BSSID     string  `json:"bssid"`
	Channel   int     `json:"ch"`
	RSSI      int     `json:"rssi"`
	Sightings int     `json:"seen"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Day       string  `json:"day,omitempty"`
	GMaps     string  `json:"gmaps"`
}

func rssiColor(rssi int) string {
	switch {
	case rssi >= -60:
		return "#3ddc84"
	case rssi >= -75:
		return "#ffb020"
	default:
		return "#ff5252"
	}
}

func BuildMapHTML(d MapData) string {
	pts := make([]mapJSONPoint, 0, len(d.Points))
	for _, p := range d.Points {
		pts = append(pts, mapJSONPoint{
			SSID: p.SSID, BSSID: p.BSSID, Channel: p.Channel, RSSI: p.RSSI,
			Sightings: p.Sightings, Lat: p.Lat, Lon: p.Lon, Day: p.Day,
			GMaps: GoogleMapsURL(p.Lat, p.Lon),
		})
	}
	ptsJSON, _ := json.Marshal(pts)
	trkJSON, _ := json.Marshal(d.Track)

	var rows strings.Builder
	for _, p := range d.Points {
		fmt.Fprintf(&rows,
			`<tr><td>%s</td><td class="mono">%s</td><td>%d</td><td style="color:%s">%d dBm</td><td>%d</td><td class="mono">%.6f, %.6f</td><td>%s</td><td><a href="%s" target="_blank" rel="noopener">Google Maps &#8599;</a></td></tr>`+"\n",
			html.EscapeString(p.SSID), html.EscapeString(p.BSSID), p.Channel,
			rssiColor(p.RSSI), p.RSSI, p.Sightings, p.Lat, p.Lon,
			html.EscapeString(p.Day), GoogleMapsURL(p.Lat, p.Lon))
	}

	days := strings.Join(d.Days, ", ")
	if days == "" {
		days = "-"
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AEGIS Wardrive Map</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css">
<style>
 body{margin:0;background:#0b0f16;color:#e8eaf0;font:14px/1.45 system-ui,sans-serif}
 header{padding:14px 18px;border-bottom:1px solid #2a344e}
 header h1{margin:0 0 4px;font-size:19px} header p{margin:0;color:#8a94a8}
 #map{height:62vh;background:#0e131d}
 .wrap{padding:14px 18px}
 table{border-collapse:collapse;width:100%%;font-size:13px}
 th,td{border-bottom:1px solid #232c42;padding:6px 8px;text-align:left}
 th{color:#8a94a8;font-weight:600} .mono{font-family:ui-monospace,monospace}
 a{color:#35d3f4} tr:hover{background:#141a26}
 .legend span{display:inline-block;margin-right:14px}
 .dot{display:inline-block;width:9px;height:9px;border-radius:50%%;margin-right:5px}
</style></head><body>
<header><h1>AEGIS Wardrive Map</h1>
<p>%d GPS-tagged network(s) &middot; %d route point(s), %.2f km &middot; day(s): %s<br>
Each pin sits where that network's signal was <b>strongest</b> when seen. Click a pin for details and a Google Maps link.
<span class="legend"><span><i class="dot" style="background:#3ddc84"></i>strong (&ge;-60 dBm)</span><span><i class="dot" style="background:#ffb020"></i>medium</span><span><i class="dot" style="background:#ff5252"></i>weak</span></span></p></header>
<div id="map"></div>
<div class="wrap"><table><thead><tr><th>Network (SSID)</th><th>BSSID</th><th>Ch</th><th>Best signal</th><th>Seen</th><th>Best-sighting position</th><th>Day</th><th></th></tr></thead>
<tbody>%s</tbody></table>
<p style="color:#5b6578">Generated locally by AEGIS Companion from your own synced library. Nothing was uploaded; map tiles load from OpenStreetMap/CARTO only when this page is online.</p></div>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<script>
var pts = %s;
var track = %s;
function esc(s){return String(s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
function col(r){return r>=-60?'#3ddc84':(r>=-75?'#ffb020':'#ff5252');}
var map = L.map('map');
var dark = L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png',{attribution:'&copy; OpenStreetMap contributors &copy; CARTO',maxZoom:19});
var streets = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{attribution:'&copy; OpenStreetMap contributors',maxZoom:19});
dark.addTo(map);
L.control.layers({'Dark':dark,'Streets':streets}).addTo(map);
var bounds = [];
pts.forEach(function(p){
  var m = L.circleMarker([p.lat,p.lon],{radius:6,color:col(p.rssi),weight:2,fillColor:col(p.rssi),fillOpacity:0.55}).addTo(map);
  m.bindPopup('<b>'+esc(p.ssid||'(hidden)')+'</b><br><span style="font-family:monospace">'+esc(p.bssid)+'</span><br>Channel '+p.ch+' &middot; best '+p.rssi+' dBm &middot; seen '+p.seen+'x<br><a href="'+p.gmaps+'" target="_blank" rel="noopener">Open in Google Maps &#8599;</a>');
  bounds.push([p.lat,p.lon]);
});
if(track && track.length>1){
  var line = L.polyline(track,{color:'#35d3f4',weight:3,opacity:0.85}).addTo(map);
  bounds = bounds.concat(track);
}
if(bounds.length){ map.fitBounds(bounds,{padding:[24,24]}); } else { map.setView([0,0],2); }
</script></body></html>`,
		len(d.Points), len(d.Track), d.RouteKm, html.EscapeString(days),
		rows.String(), ptsJSON, trkJSON)
}

// WriteMapHTML regenerates the interactive map inside the library and
// returns its path.
func WriteMapHTML(root string, d MapData) (string, error) {
	dir := filepath.Join(root, "maps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "wardrive-map.html")
	return path, os.WriteFile(path, []byte(BuildMapHTML(d)), 0o644)
}
