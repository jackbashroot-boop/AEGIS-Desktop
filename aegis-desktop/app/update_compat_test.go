package main

import (
	"encoding/json"
	"testing"
)

// Regression (v1.8.10): the suite-era feeds put the SUITE version in
// "version" (always below Companion 1.8.x, so every install reported
// "up to date" forever) and used "file" inside entries (which the
// parser did not read, so no installer could be named either). The
// parser must resolve every shape ever shipped, and effVersion must
// prefer the explicit companion field.
func TestFeedCompatShapes(t *testing.T) {
	// Suite-era shape (v1.2.15-1.2.21): suite version + "file" keys.
	old := []byte(`{"version":"1.2.21","files":{"linux":{"file":"Aegis-Suite-Setup-Linux","sha256":"aa","size":10},"windows":{"file":"Aegis-Suite-Setup-Windows.exe","sha256":"bb","size":11}},"linux":{"file":"Aegis-Suite-Setup-Linux","sha256":"aa","size":10},"windows":{"file":"Aegis-Suite-Setup-Windows.exe","sha256":"bb","size":11}}`)
	var l updateLatest
	if err := json.Unmarshal(old, &l); err != nil {
		t.Fatal(err)
	}
	if l.effectiveFile() == "" {
		t.Fatal("suite-era shape: no installer named")
	}
	if l.effectiveSHA256() == "" {
		t.Fatal("suite-era shape: no sha")
	}
	if l.effVersion() != "1.2.21" {
		t.Fatalf("effVersion fallback wrong: %q", l.effVersion())
	}

	// New shape: companion version rides in version + companion.
	nw := []byte(`{"version":"1.8.10","companion":"1.8.10","suite":"1.2.22","files":{"linux":{"name":"Aegis-Suite-Setup-Linux","file":"Aegis-Suite-Setup-Linux","sha256":"aa","size":10},"windows":{"name":"Aegis-Suite-Setup-Windows.exe","file":"Aegis-Suite-Setup-Windows.exe","sha256":"bb","size":11}},"file_linux":"Aegis-Suite-Setup-Linux","sha256_linux":"aa"}`)
	var m updateLatest
	if err := json.Unmarshal(nw, &m); err != nil {
		t.Fatal(err)
	}
	if m.effVersion() != "1.8.10" {
		t.Fatalf("effVersion = %q", m.effVersion())
	}
	if !versionNewer(m.effVersion(), "1.8.2") {
		t.Fatal("stuck 1.8.2 would not see 1.8.10 as newer")
	}
	if versionNewer("1.2.21", "1.8.2") {
		t.Fatal("suite version must NOT count as newer (that was the bug)")
	}
}
