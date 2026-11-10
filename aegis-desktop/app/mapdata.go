package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Wardrive map data: every GPS-tagged sighting the library holds, merged
// across days and export formats. PHOENIX records, per network, the
// position where the signal was strongest ("best sighting"), so a point
// on the map answers "where was this network loudest?" - the standard
// wardrive interpretation (WiGLE/KML/GeoJSON exports agree).

type MapPoint struct {
	SSID      string
	BSSID     string
	Channel   int
	RSSI      int
	Sightings int
	Lat, Lon  float64
	Day       string // library day the record came from, "" if unknown
	Source    string // file the winning record came from
}

type MapData struct {
	Points               []MapPoint // one per BSSID (strongest sighting), RSSI desc
	Track                [][2]float64
	Days                 []string
	Files                int // map-bearing files read
	NoGPS                int // sighting records that had no GPS fix
	RouteKm              float64
	CenterLat, CenterLon float64
}

func validLatLon(lat, lon float64) bool {
	return lat != 0 && lon != 0 && math.Abs(lat) <= 90 && math.Abs(lon) <= 180
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// GoogleMapsURL is the share/search URL that opens Google Maps pinned at
// the coordinate - no API key needed, works signed-out.
func GoogleMapsURL(lat, lon float64) string {
	return fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%.6f,%.6f", lat, lon)
}

var daySegRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func dayOf(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if daySegRe.MatchString(seg) {
			return seg
		}
	}
	return ""
}

// LoadMapData walks a companion Library (or any folder tree containing
// device raw folders) and merges every map-bearing file it finds:
// wifi_map.geojson / *.geojson, wifi_map.csv, WiGLE CSVs, wardrive
// .jsonl records, and GPX tracks. Unknown files are ignored.
func LoadMapData(root string) MapData {
	var d MapData
	best := map[string]MapPoint{}
	daySet := map[string]bool{}
	var track [][2]float64

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		var pts []MapPoint
		var trk [][2]float64
		var noGPS int
		switch {
		case strings.HasSuffix(name, ".geojson"):
			pts = parseGeoJSON(path)
		case strings.HasSuffix(name, ".gpx"):
			trk = parseGPX(path)
		case strings.HasSuffix(name, ".csv"):
			pts = parseMapCSV(path)
		case strings.HasSuffix(name, ".jsonl"):
			pts, noGPS = parseSightingJSONL(path)
		default:
			return nil
		}
		if len(pts) == 0 && len(trk) == 0 && noGPS == 0 {
			return nil
		}
		d.Files++
		day := dayOf(root, path)
		if day != "" {
			daySet[day] = true
		}
		d.NoGPS += noGPS
		for _, p := range pts {
			p.Day = day
			p.Source = info.Name()
			key := strings.ToUpper(p.BSSID)
			if key == "" {
				continue
			}
			old, ok := best[key]
			if !ok || p.RSSI > old.RSSI {
				if ok && old.Sightings > p.Sightings {
					p.Sightings = old.Sightings
				}
				best[key] = p
			} else if p.Sightings > old.Sightings {
				old.Sightings = p.Sightings
				best[key] = old
			}
		}
		track = append(track, trk...)
		return nil
	})

	for _, p := range best {
		d.Points = append(d.Points, p)
	}
	sort.Slice(d.Points, func(i, j int) bool {
		if d.Points[i].RSSI != d.Points[j].RSSI {
			return d.Points[i].RSSI > d.Points[j].RSSI
		}
		return d.Points[i].BSSID < d.Points[j].BSSID
	})
	// drop consecutive duplicate track points (GPS parked = same fix)
	for _, tp := range track {
		if n := len(d.Track); n > 0 && d.Track[n-1] == tp {
			continue
		}
		d.Track = append(d.Track, tp)
	}
	for i := 1; i < len(d.Track); i++ {
		d.RouteKm += haversineKm(d.Track[i-1][0], d.Track[i-1][1], d.Track[i][0], d.Track[i][1])
	}
	for day := range daySet {
		d.Days = append(d.Days, day)
	}
	sort.Strings(d.Days)
	if len(d.Points) > 0 {
		var la, lo float64
		for _, p := range d.Points {
			la += p.Lat
			lo += p.Lon
		}
		d.CenterLat = la / float64(len(d.Points))
		d.CenterLon = lo / float64(len(d.Points))
	} else if len(d.Track) > 0 {
		d.CenterLat, d.CenterLon = d.Track[0][0], d.Track[0][1]
	}
	return d
}

// ---- parsers -----------------------------------------------------------

type geojsonFile struct {
	Features []struct {
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]any `json:"properties"`
	} `json:"features"`
}

func propStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func propInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case string:
				if i, err := strconv.Atoi(strings.TrimSpace(strings.Split(n, ",")[0])); err == nil {
					return i
				}
			}
		}
	}
	return 0
}

func parseGeoJSON(path string) []MapPoint {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var gj geojsonFile
	if json.Unmarshal(raw, &gj) != nil {
		return nil
	}
	var out []MapPoint
	for _, f := range gj.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		lon, lat := f.Geometry.Coordinates[0], f.Geometry.Coordinates[1]
		if !validLatLon(lat, lon) {
			continue
		}
		out = append(out, MapPoint{
			SSID:      propStr(f.Properties, "ssid", "SSID", "name"),
			BSSID:     propStr(f.Properties, "bssid", "BSSID", "mac"),
			Channel:   propInt(f.Properties, "channel", "channels", "ch"),
			RSSI:      propInt(f.Properties, "best_rssi", "rssi", "RSSI"),
			Sightings: propInt(f.Properties, "sightings", "seen"),
			Lat:       lat, Lon: lon,
		})
	}
	return out
}

func parseMapCSV(path string) []MapPoint {
	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	r := csv.NewReader(bufio.NewReader(fh))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return nil
	}
	// WiGLE files open with a WigleWifi-1.4 pre-header line.
	if strings.HasPrefix(rows[0][0], "WigleWifi") {
		rows = rows[1:]
	}
	if len(rows) == 0 {
		return nil
	}
	header := map[string]int{}
	for i, h := range rows[0] {
		header[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(row []string, names ...string) string {
		for _, n := range names {
			if i, ok := header[n]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
		}
		return ""
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	atof := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	var out []MapPoint
	for _, row := range rows[1:] {
		lat := atof(get(row, "lat", "currentlatitude", "latitude"))
		lon := atof(get(row, "lon", "lng", "currentlongitude", "longitude"))
		if !validLatLon(lat, lon) {
			continue
		}
		out = append(out, MapPoint{
			SSID:      get(row, "ssid"),
			BSSID:     get(row, "bssid", "mac"),
			Channel:   atoi(get(row, "channel", "ch")),
			RSSI:      atoi(get(row, "best_rssi", "rssi")),
			Sightings: atoi(get(row, "sightings")),
			Lat:       lat, Lon: lon,
		})
	}
	return out
}

// parseSightingJSONL reads one-JSON-object-per-line logs and returns the
// records carrying a BSSID (wardrive sightings). Records without a valid
// fix are counted separately so the UI can say "N sightings had no GPS".
func parseSightingJSONL(path string) (pts []MapPoint, noGPS int) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		bssid := propStr(m, "bssid", "BSSID", "mac")
		if bssid == "" {
			continue
		}
		lat := propFloat(m, "lat", "latitude")
		lon := propFloat(m, "lon", "lng", "longitude")
		if !validLatLon(lat, lon) {
			noGPS++
			continue
		}
		pts = append(pts, MapPoint{
			SSID:      propStr(m, "ssid", "SSID"),
			BSSID:     bssid,
			Channel:   propInt(m, "ch", "channel"),
			RSSI:      propInt(m, "rssi", "RSSI"),
			Sightings: 1,
			Lat:       lat, Lon: lon,
		})
	}
	return pts, noGPS
}

func propFloat(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return n
			case string:
				if f, err := strconv.ParseFloat(n, 64); err == nil {
					return f
				}
			}
		}
	}
	return 0
}

var trkptRe = regexp.MustCompile(`<trkpt[^>]*lat="(-?[0-9.]+)"[^>]*lon="(-?[0-9.]+)"`)

func parseGPX(path string) [][2]float64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out [][2]float64
	for _, m := range trkptRe.FindAllSubmatch(raw, -1) {
		lat, err1 := strconv.ParseFloat(string(m[1]), 64)
		lon, err2 := strconv.ParseFloat(string(m[2]), 64)
		if err1 == nil && err2 == nil && validLatLon(lat, lon) {
			out = append(out, [2]float64{lat, lon})
		}
	}
	return out
}
