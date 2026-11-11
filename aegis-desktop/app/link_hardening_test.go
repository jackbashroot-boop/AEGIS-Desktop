package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

// v1.8.5: an empty wardrive must still frame the full 100x100-mile
// home map, and the discovery shout must include the limited
// broadcast even on machines with no usable interfaces.

func TestViewBoundsFallsBackToFullHomeMap(t *testing.T) {
	minLat, minLon, maxLat, maxLon := viewBounds(MapData{}, 38.0, -77.0, true)
	if !finite4(minLat, minLon, maxLat, maxLon) {
		t.Fatalf("fallback bounds not finite: %v %v %v %v", minLat, minLon, maxLat, maxLon)
	}
	// 100 miles tall/wide, centred on home.
	if math.Abs((maxLat-minLat)-2*areaHalfMiles/69.0) > 0.01 {
		t.Errorf("fallback height = %v deg, want the 100-mile box", maxLat-minLat)
	}
	if math.Abs((minLat+maxLat)/2-38.0) > 0.01 || math.Abs((minLon+maxLon)/2-(-77.0)) > 0.01 {
		t.Errorf("fallback not centred on home: %v..%v, %v..%v", minLat, maxLat, minLon, maxLon)
	}
	// No fallback known: bounds stay infinite (callers show no map).
	a, b, c, e := viewBounds(MapData{}, 0, 0, false)
	if finite4(a, b, c, e) {
		t.Errorf("no-fallback bounds should stay infinite, got %v %v %v %v", a, b, c, e)
	}
	// Real data always wins over the fallback.
	d := MapData{Points: []MapPoint{{Lat: 40, Lon: -70}, {Lat: 41, Lon: -71}}}
	a, b, c, e = viewBounds(d, 38, -77, true)
	if a != 40 || c != 41 || b != -71 || e != -70 {
		t.Errorf("data bounds lost to fallback: %v %v %v %v", a, b, c, e)
	}
}

func TestMapHTMLCentresOnHomeWhenEmpty(t *testing.T) {
	html := BuildMapHTML(MapData{CenterLat: 38.5, CenterLon: -77.25})
	if !strings.Contains(html, "homeC") || !strings.Contains(html, "[38.5,-77.25]") {
		t.Errorf("empty map html missing home centre")
	}
	if !strings.Contains(html, "no sightings here yet") {
		t.Errorf("empty map html missing home marker popup")
	}
}

// v1.8.6: a reply to the previous ping (nonce rotated since) must
// still verify inside its window, and SetToken must take effect
// immediately (the stale-token bug emptied "On your network").
func TestDiscoveryProofRecentNoncesAndSetToken(t *testing.T) {
	d := NewDiscovery("tok-one", NewLog())
	d.nonces = []pingNonceRec{{nonce: "nonce-old", at: time.Now()}}
	if !d.proofOK(pingMAC("tok-one", "nonce-old")) {
		t.Error("proof for recent rotated nonce rejected")
	}
	if d.proofOK(pingMAC("tok-two", "nonce-old")) {
		t.Error("proof under another token accepted")
	}
	d.SetToken("tok-two")
	if d.proofOK(pingMAC("tok-one", "nonce-old")) {
		t.Error("old-token proof still accepted after SetToken")
	}
	if !d.proofOK(pingMAC("tok-two", "nonce-old")) {
		t.Error("new-token proof rejected after SetToken")
	}
	d.nonces = []pingNonceRec{{nonce: "nonce-stale", at: time.Now().Add(-3 * time.Minute)}}
	if d.proofOK(pingMAC("tok-two", "nonce-stale")) {
		t.Error("expired nonce accepted")
	}
	if n, _, _ := d.Rejects(); n != 0 {
		t.Errorf("Rejects() = %d before any reject", n)
	}
	d.recordReject("10.0.0.99")
	if n, ip, _ := d.Rejects(); n != 1 || ip != "10.0.0.99" {
		t.Errorf("Rejects() = %d, %q after one reject", n, ip)
	}
}

func TestPingBroadcastAddrsAlwaysHasLimitedBroadcast(t *testing.T) {
	addrs := pingBroadcastAddrs()
	if len(addrs) == 0 {
		t.Fatal("no broadcast addrs")
	}
	found := false
	for _, a := range addrs {
		if a.Port != 8791 {
			t.Errorf("broadcast addr %v not on the device ping port", a)
		}
		if a.IP.String() == "255.255.255.255" {
			found = true
		}
	}
	if !found {
		t.Errorf("limited broadcast missing from %v", addrs)
	}
}
