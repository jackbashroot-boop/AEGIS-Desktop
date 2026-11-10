package main

import (
	"encoding/json"
	"testing"
)

// The release generator's latest.json shape must parse into a
// downloadable, verifiable update (regression: nested "files" was
// ignored before v1.7.9, so Install always failed).
func TestLatestJSONShippedShape(t *testing.T) {
	raw := []byte(`{
  "version": "1.7.9",
  "files": {
    "linux":   {"name": "Aegis-Suite-Setup-Linux", "sha256": "aaaa", "size": 12345},
    "windows": {"name": "Aegis-Suite-Setup-Windows.exe", "sha256": "bbbb", "size": 12346}
  },
  "notes": "test"
}`)
	var l updateLatest
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	if l.effectiveFile() == "" {
		t.Fatal("effectiveFile empty for shipped latest.json shape")
	}
	if l.effectiveSHA256() == "" {
		t.Fatal("effectiveSHA256 empty for shipped latest.json shape")
	}
	if l.effectiveSize() <= 0 {
		t.Fatal("effectiveSize not read")
	}
	// Flat legacy shape still works.
	var flat updateLatest
	if err := json.Unmarshal([]byte(`{"version":"1.0.0","file_linux":"L.bin","sha256_linux":"cc"}`), &flat); err != nil {
		t.Fatal(err)
	}
	if flat.effectiveSHA256() != "cc" {
		t.Fatal("flat shape broken")
	}
}
