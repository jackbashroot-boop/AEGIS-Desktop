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

	// Location-quality metadata (v1.8.4). Lat/Lon is no longer always
	// one raw GPS point: when several sightings exist, LoadMapData
	// robustly combines the strong, low-noise fixes and reports the
	// resulting uncertainty instead of pretending a pin is exact.
	AccuracyM    float64 // source-reported/estimated GPS error of the best fix, metres
	UncertaintyM float64 // estimated radius around Lat/Lon, metres
	Observations int     // valid GPS sightings combined into this estimate
	Sats         int
	HDOP         float64
	Method       string
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
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return false
	}
	// (0,0) is the universal "no fix" sentinel, but a real point may
	// legitimately sit on the equator or the prime meridian.
	if lat == 0 && lon == 0 {
		return false
	}
	return math.Abs(lat) <= 90 && math.Abs(lon) <= 180
}

// observationAccuracyM turns whatever quality evidence a record has
// (reported accuracy, HDOP, satellites, or nothing) into a comparable
// one-sigma-ish GPS error in metres. Unknown consumer GPS is assumed
// mediocre, never magically precise.
func observationAccuracyM(p MapPoint) float64 {
	if p.AccuracyM > 0 && !math.IsNaN(p.AccuracyM) && !math.IsInf(p.AccuracyM, 0) {
		return math.Max(3, math.Min(1000, p.AccuracyM))
	}
	if p.HDOP > 0 && p.HDOP < 20 {
		return math.Max(5, math.Min(250, p.HDOP*5))
	}
	switch {
	case p.Sats >= 12:
		return 15
	case p.Sats >= 8:
		return 25
	case p.Sats >= 5:
		return 45
	case p.Sats >= 4:
		return 70
	default:
		return 90
	}
}

func medianFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	v := append([]float64(nil), vals...)
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

func distanceM(lat1, lon1, lat2, lon2 float64) float64 {
	return haversineKm(lat1, lon1, lat2, lon2) * 1000
}

// estimateMapPoint combines every valid sighting of one BSSID. The
// strongest sighting anchors the estimate (for Wi-Fi, loudest usually
// means nearest), nearby strong sightings refine it, and a lone GPS
// spike is rejected when a cluster of other strong fixes disagrees.
// Weights favour both stronger RSSI and lower GPS error. This is an
// estimate, not triangulation magic: UncertaintyM says how far the
// pin may reasonably be from the access point.
func estimateMapPoint(obs []MapPoint) MapPoint {
	valid := make([]MapPoint, 0, len(obs))
	for _, p := range obs {
		if validLatLon(p.Lat, p.Lon) {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return MapPoint{}
	}
	best := valid[0]
	for _, p := range valid[1:] {
		pa, ba := observationAccuracyM(p), observationAccuracyM(best)
		if p.RSSI > best.RSSI || (p.RSSI == best.RSSI && pa < ba) {
			best = p
		}
	}
	out := best
	out.AccuracyM = observationAccuracyM(best)
	out.UncertaintyM = out.AccuracyM
	out.Observations = 1
	out.Method = "strongest GPS fix"
	if len(valid) == 1 {
		out.Method = "single GPS fix"
		return out
	}

	// Detect a strongest-point GPS spike: compare it with the median
	// position of all other strong sightings. If four or more strong
	// fixes cluster somewhere else, trust the cluster.
	anchorLat, anchorLon := best.Lat, best.Lon
	anchorIsOutlier := false
	var strong []MapPoint
	for _, p := range valid {
		if p.RSSI >= best.RSSI-25 {
			strong = append(strong, p)
		}
	}
	if len(strong) >= 4 {
		var lats, lons []float64
		for _, p := range strong {
			lats = append(lats, p.Lat)
			lons = append(lons, p.Lon)
		}
		medLat, medLon := medianFloat(lats), medianFloat(lons)
		if distanceM(best.Lat, best.Lon, medLat, medLon) > 250 {
			anchorLat, anchorLon = medLat, medLon
			anchorIsOutlier = true
		}
	}

	radius := math.Max(150, math.Min(600, 4*out.AccuracyM))
	if anchorIsOutlier {
		radius = 300
	}
	var cand []MapPoint
	for _, p := range valid {
		if p.RSSI < best.RSSI-30 {
			continue
		}
		if distanceM(p.Lat, p.Lon, anchorLat, anchorLon) <= radius {
			cand = append(cand, p)
		}
	}
	if len(cand) == 0 {
		cand = []MapPoint{best}
	}
	if len(cand) == 1 {
		out = cand[0]
		out.AccuracyM = observationAccuracyM(cand[0])
		out.UncertaintyM = out.AccuracyM
		out.Observations = 1
		if anchorIsOutlier {
			out.Method = "GPS cluster (strongest point rejected)"
		}
		// Keep the globally strongest metadata even if its position was rejected.
		out.RSSI = best.RSSI
		return out
	}

	cosLat := math.Cos(anchorLat * math.Pi / 180)
	if math.Abs(cosLat) < 0.05 {
		if cosLat < 0 {
			cosLat = -0.05
		} else {
			cosLat = 0.05
		}
	}
	var sumW, sumX, sumY, sumAcc2 float64
	for _, p := range cand {
		acc := observationAccuracyM(p)
		signalW := math.Pow(10, float64(p.RSSI-best.RSSI)/15)
		w := signalW / (acc + 10)
		x := (p.Lon - anchorLon) * 111320 * cosLat
		y := (p.Lat - anchorLat) * 110540
		sumW += w
		sumX += w * x
		sumY += w * y
		sumAcc2 += w * acc * acc
	}
	estX, estY := sumX/sumW, sumY/sumW
	// Do not let averaging drag a good anchor implausibly far away.
	if !anchorIsOutlier {
		if d := math.Hypot(estX, estY); d > 250 {
			estX *= 250 / d
			estY *= 250 / d
		}
	}
	estLat := anchorLat + estY/110540
	estLon := anchorLon + estX/(111320*cosLat)
	var spread2 float64
	for _, p := range cand {
		acc := observationAccuracyM(p)
		signalW := math.Pow(10, float64(p.RSSI-best.RSSI)/15)
		w := signalW / (acc + 10)
		x := (p.Lon - anchorLon) * 111320 * cosLat
		y := (p.Lat - anchorLat) * 110540
		spread2 += w * ((x-estX)*(x-estX) + (y-estY)*(y-estY))
	}
	uncertainty := math.Sqrt(spread2/sumW + sumAcc2/sumW)
	out.Lat, out.Lon = estLat, estLon
	out.UncertaintyM = math.Max(10, math.Min(750, uncertainty))
	out.Observations = len(cand)
	out.Method = "multi-fix weighted"
	if anchorIsOutlier {
		out.Method = "multi-fix weighted (strongest point rejected)"
	}
	// Sightings in aggregate files can describe the same raw records;
	// retain the largest count instead of double-counting formats.
	for _, p := range valid {
		if p.Sightings > out.Sightings {
			out.Sightings = p.Sightings
		}
	}
	return out
}

func cleanTrack(track [][2]float64) [][2]float64 {
	var out [][2]float64
	for i, tp := range track {
		if !validLatLon(tp[0], tp[1]) {
			continue
		}
		if n := len(out); n > 0 && distanceM(out[n-1][0], out[n-1][1], tp[0], tp[1]) < 2 {
			continue
		}
		// Reject an isolated GPS spike when the route immediately
		// returns near the previous accepted point.
		if n := len(out); n > 0 && i+1 < len(track) {
			prev := out[n-1]
			next := track[i+1]
			if distanceM(prev[0], prev[1], tp[0], tp[1]) > 2000 && distanceM(prev[0], prev[1], next[0], next[1]) < 2000 {
				continue
			}
		}
		out = append(out, tp)
	}
	return out
}

func robustCenter(points []MapPoint, track [][2]float64) (float64, float64) {
	if len(points) > 0 {
		var lats, lons []float64
		for _, p := range points {
			lats = append(lats, p.Lat)
			lons = append(lons, p.Lon)
		}
		return medianFloat(lats), medianFloat(lons)
	}
	if len(track) > 0 {
		return track[0][0], track[0][1]
	}
	return 0, 0
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
	observations := map[string][]MapPoint{}
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
			observations[key] = append(observations[key], p)
		}
		track = append(track, trk...)
		return nil
	})

	for _, group := range observations {
		d.Points = append(d.Points, estimateMapPoint(group))
	}
	sort.Slice(d.Points, func(i, j int) bool {
		if d.Points[i].RSSI != d.Points[j].RSSI {
			return d.Points[i].RSSI > d.Points[j].RSSI
		}
		return d.Points[i].BSSID < d.Points[j].BSSID
	})
	d.Track = cleanTrack(track)
	for i := 1; i < len(d.Track); i++ {
		d.RouteKm += haversineKm(d.Track[i-1][0], d.Track[i-1][1], d.Track[i][0], d.Track[i][1])
	}
	for day := range daySet {
		d.Days = append(d.Days, day)
	}
	sort.Strings(d.Days)
	d.CenterLat, d.CenterLon = robustCenter(d.Points, d.Track)
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
			AccuracyM: propFloat(f.Properties, "accuracy_m", "accuracyMeters", "AccuracyMeters", "accuracy"),
			Sats:      propInt(f.Properties, "sats", "satellites"),
			HDOP:      propFloat(f.Properties, "hdop", "HDOP"),
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
			AccuracyM: atof(get(row, "accuracymeters", "accuracy_m", "accuracy", "gps_accuracy")),
			Sats:      atoi(get(row, "sats", "satellites")),
			HDOP:      atof(get(row, "hdop")),
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
			AccuracyM: propFloat(m, "acc", "accuracy_m", "accuracy", "AccuracyMeters"),
			Sats:      propInt(m, "sats", "satellites"),
			HDOP:      propFloat(m, "hdop", "HDOP"),
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
