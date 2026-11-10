package main

// ZIP-code home location (v1.8.0, user request): instead of forcing
// latitude/longitude, Settings accepts a US ZIP code and resolves it
// to coordinates with OpenStreetMap's Nominatim service. That needs
// internet ONCE, at setup time; the resulting lat/lon is what's saved
// and used for the offline home map, exactly like typed coordinates.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// nominatimBase is a var so tests can point at a local server.
var nominatimBase = "https://nominatim.openstreetmap.org/search"

// normalizeZip accepts "06360" or "06360-1234" (ZIP+4 resolves to the
// 5-digit ZIP). Anything else is rejected before any network traffic.
func normalizeZip(s string) (string, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	if len(s) != 5 {
		return "", fmt.Errorf("ZIP code should be 5 digits (for example 06360) - or use latitude/longitude below")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("ZIP code should be 5 digits (for example 06360) - or use latitude/longitude below")
		}
	}
	return s, nil
}

// geocodeZip resolves a normalized 5-digit US ZIP to (lat, lon, place).
// Errors are plain English on purpose: Settings prints them verbatim.
func geocodeZip(ctx context.Context, zip string) (float64, float64, string, error) {
	lat, lon, place, _, err := geocodeZipDetailed(ctx, zip)
	return lat, lon, place, err
}

// geocodeZipDetailed also estimates how uncertain the ZIP centre is.
// A ZIP code is an area, not a street address: Nominatim's bounding
// box gives a defensible radius (half its diagonal, bounded so a
// rural ZIP does not pretend to be exact and a city ZIP does not
// claim hundreds of kilometres).
func geocodeZipDetailed(ctx context.Context, zip string) (float64, float64, string, float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nominatimBase+"?format=jsonv2&limit=1&countrycodes=us&postalcode="+zip, nil)
	if err != nil {
		return 0, 0, "", 0, err
	}
	req.Header.Set("User-Agent", "AegisCompanion/"+appVersion+" (offline home-map setup)")
	cl := &http.Client{Timeout: 12 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, 0, "", 0, fmt.Errorf("could not look up ZIP %s - check your internet connection, or type latitude/longitude instead", zip)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", 0, fmt.Errorf("the ZIP lookup service answered %s - try again later, or type latitude/longitude instead", resp.Status)
	}
	var rows []struct {
		Lat         string   `json:"lat"`
		Lon         string   `json:"lon"`
		DisplayName string   `json:"display_name"`
		BoundingBox []string `json:"boundingbox"` // south, north, west, east
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil || len(rows) == 0 {
		return 0, 0, "", 0, fmt.Errorf("ZIP %s was not found - check the digits, or type latitude/longitude instead", zip)
	}
	lat, err1 := strconv.ParseFloat(rows[0].Lat, 64)
	lon, err2 := strconv.ParseFloat(rows[0].Lon, 64)
	if err1 != nil || err2 != nil || lat < -85 || lat > 85 || lon < -180 || lon > 180 {
		return 0, 0, "", 0, fmt.Errorf("ZIP %s resolved to nonsense coordinates - type latitude/longitude instead", zip)
	}
	accuracyM := 5000.0
	if len(rows[0].BoundingBox) == 4 {
		south, e1 := strconv.ParseFloat(rows[0].BoundingBox[0], 64)
		north, e2 := strconv.ParseFloat(rows[0].BoundingBox[1], 64)
		west, e3 := strconv.ParseFloat(rows[0].BoundingBox[2], 64)
		east, e4 := strconv.ParseFloat(rows[0].BoundingBox[3], 64)
		if e1 == nil && e2 == nil && e3 == nil && e4 == nil && validLatLon(south, west) && validLatLon(north, east) {
			diagKm := haversineKm(south, west, north, east)
			if !math.IsNaN(diagKm) && !math.IsInf(diagKm, 0) && diagKm > 0 {
				accuracyM = math.Max(500, math.Min(25000, diagKm*1000/2))
			}
		}
	}
	place := rows[0].DisplayName
	if i := strings.Index(place, ","); i > 0 && len(place) > 60 {
		place = place[:60] + "..."
	}
	return lat, lon, place, accuracyM, nil
}
