package main

import "testing"

func TestParseNmcli(t *testing.T) {
	in := "AA\\:BB\\:CC\\:DD\\:EE\\:FF:Duffwifi:87\n11\\:22\\:33\\:44\\:55\\:66:Neighbor Net:50\n"
	got := parseNmcli(in)
	if len(got) != 2 {
		t.Fatalf("want 2 APs, got %d (%+v)", len(got), got)
	}
	if got[0].SSID != "Duffwifi" || got[0].RSSI != -57 {
		t.Fatalf("first AP wrong: %+v", got[0])
	}
	if got[1].SSID != "Neighbor Net" {
		t.Fatalf("SSID with space wrong: %+v", got[1])
	}
}

func TestParseNetsh(t *testing.T) {
	in := "SSID 1 : Duffwifi\n    BSSID 1 : aa:bb:cc:dd:ee:ff\n         Signal : 80%\n"
	got := parseNetsh(in)
	if len(got) != 1 || got[0].RSSI != -60 || got[0].BSSID != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("netsh parse wrong: %+v", got)
	}
}

func TestParseHomeDevicesAndRepo(t *testing.T) {
	ps := parseHomeDevices("Dad phone=aa:bb:cc:dd:ee:ff\nbad line\nTV = 11:22:33:44:55:66\n")
	if len(ps) != 2 || ps[0].MAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("home devices wrong: %+v", ps)
	}
	if repoFromFeed("https://github.com/jackbashroot-boop/AEGIS-Desktop/releases/latest/download/latest.json") != "jackbashroot-boop/AEGIS-Desktop" {
		t.Fatal("repo parse wrong")
	}
	if repoFromFeed("https://example.com/x.json") != "" {
		t.Fatal("non-github feed must be empty")
	}
}
