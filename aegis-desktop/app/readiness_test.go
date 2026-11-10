package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildReadinessReportRedactsSecretsAndShowsUncertainty(t *testing.T) {
	cfg := &Config{
		Port: 8791, Token: "SECRET_TOKEN_SHOULD_NOT_APPEAR", Library: "/tmp/Library",
		HomeLat: 41.5247, HomeLon: -72.0759, HomeAccuracyM: 3200, HomeSource: "ZIP code area centre",
		HomeSSID: "Duffwifi",
	}
	report := buildReadinessReport(readinessData{
		Now: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC), OS: "linux", Arch: "amd64", Cfg: cfg,
		Health:  []HealthItem{{Label: "Listener", Detail: "accepting", OK: true}},
		Map:     MapData{Points: []MapPoint{{SSID: "Duffwifi", BSSID: "AA", RSSI: -40, Lat: 41.5247, Lon: -72.0759, UncertaintyM: 42, Observations: 5}}, NoGPS: 2},
		Devices: []DeviceLive{{Prog: "PHOENIX", Ver: "0.12.7", State: "scanning", Batt: 80, HasBatt: true, GPSAcc: 18, HasGPSAcc: true, GPSRej: 1, HasGPSRej: true}},
	})
	if strings.Contains(report, cfg.Token) {
		t.Fatal("readiness report leaked token")
	}
	for _, want := range []string{"AEGIS FIELD READINESS REPORT", "±3200 m", "median ±42 m", "GPS estimate: ±18 m", "no device token"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}
