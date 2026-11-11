package main

// PC location lookup (v1.8.12, user request): with the user's
// explicit OK, the Companion asks a public IP-location service
// where this internet connection appears to be, and saves that as
// the home location (city-level approximate - the starting point
// for the offline home map, refinable by ZIP, typed coordinates or
// GPS). Deny, and it never asks again; nothing is looked up or
// saved without the Allow click.
//
// Honesty notes: the service sees this connection's public IP,
// exactly like any website does. Nothing about the PC, its files,
// its devices or its data is sent - the request has no payload at
// all. The answer is approximate by nature (providers locate the
// ISP's point of presence, not your rooftop), so it is saved
// labelled "approximate" with a generous uncertainty.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ipLocBase is a var so tests can point at a local server.
var ipLocBase = "https://ipapi.co/json/"

type ipLocation struct {
	Lat, Lon float64
	Place    string // "City, Region"
}

// fetchIPLocation resolves this connection's approximate location.
// The endpoint returns JSON like
// {"latitude":41.66,"longitude":-72.69,"city":"New London","region":"Connecticut",...}
// or {"error":true,"reason":"RateLimited"} when throttled.
func fetchIPLocation(ctx context.Context) (ipLocation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipLocBase, nil)
	if err != nil {
		return ipLocation{}, err
	}
	req.Header.Set("User-Agent", "AEGIS-Companion/"+appVersion+" (home-map setup, user-approved lookup)")
	cl := &http.Client{Timeout: 15 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return ipLocation{}, fmt.Errorf("could not reach the location service (%v) - check the internet connection", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ipLocation{}, fmt.Errorf("location service answered %s - try again later, or use ZIP / coordinates", resp.Status)
	}
	var m struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		City      string  `json:"city"`
		Region    string  `json:"region"`
		Error     bool    `json:"error"`
		Reason    string  `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return ipLocation{}, fmt.Errorf("location service sent something unreadable - use ZIP / coordinates instead")
	}
	if m.Error {
		return ipLocation{}, fmt.Errorf("location service declined (%s) - use ZIP / coordinates instead", m.Reason)
	}
	if !validLatLon(m.Latitude, m.Longitude) {
		return ipLocation{}, fmt.Errorf("location service returned no usable coordinates - use ZIP / coordinates instead")
	}
	place := strings.TrimSpace(m.City + ", " + m.Region)
	if place == ", " || place == "" {
		place = "approximate location"
	}
	return ipLocation{Lat: m.Latitude, Lon: m.Longitude, Place: place}, nil
}
