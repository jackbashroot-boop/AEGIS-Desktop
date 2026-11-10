package main

// GPS refinement of the stored home point (v1.8.4). A ZIP code gives
// an area centre and typed coordinates are only as good as the source
// they were copied from. If the user walks/drives around the property
// with PHOENIX wardriving, the Companion already holds multiple GPS
// sightings of the home access point; estimateMapPoint combines the
// strongest, cleanest fixes and reports their uncertainty. That is
// the most accurate home location available without asking the user
// to survey anything by hand.

import (
	"fmt"
	"strings"
)

// RefineHomeFromSSID finds the named home network in the synced
// library and returns its multi-fix position estimate. It refuses to
// invent precision: too few usable fixes, or a very loose estimate,
// is an actionable error rather than a silently bad centre.
func RefineHomeFromSSID(root, ssid string) (lat, lon, uncertaintyM float64, fixes int, err error) {
	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		return 0, 0, 0, 0, fmt.Errorf("type your home Wi-Fi name in the Presence section first - I need to know which network is home")
	}
	d := LoadMapData(root)
	var best *MapPoint
	for i := range d.Points {
		p := &d.Points[i]
		if !strings.EqualFold(strings.TrimSpace(p.SSID), ssid) {
			continue
		}
		// Prefer evidence: more combined fixes, then stronger signal.
		if best == nil || p.Observations > best.Observations ||
			(p.Observations == best.Observations && p.RSSI > best.RSSI) {
			best = p
		}
	}
	if best == nil {
		return 0, 0, 0, 0, fmt.Errorf("no GPS-tagged sightings of %q are in the Library yet - wardrive around the property with GPS lock, sync the Cardputer, then try again", ssid)
	}
	if best.Observations < 3 {
		return 0, 0, 0, best.Observations, fmt.Errorf("only %d usable GPS fix(es) for %q so far - walk or drive past the house from a few different spots, sync again, then refine", best.Observations, ssid)
	}
	if best.UncertaintyM <= 0 || best.UncertaintyM > 500 {
		return 0, 0, best.UncertaintyM, best.Observations, fmt.Errorf("the GPS fixes for %q are too scattered (about ±%.0f m) to refine home safely - collect fixes with a clear sky view and sync again", ssid, best.UncertaintyM)
	}
	return best.Lat, best.Lon, best.UncertaintyM, best.Observations, nil
}
