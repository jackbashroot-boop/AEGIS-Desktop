package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Throwaway-style E2E (kept): the full check -> available -> download
// -> SHA-verified staged flow against a local stand-in release feed.
func TestUpdaterEndToEndLocalFeed(t *testing.T) {
	installer := []byte("fake-installer-bytes")
	sum := sha256.Sum256(installer)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "latest.json":
			fmt.Fprintf(w, `{"version":"99.0.0","file_linux":"Aegis-Suite-Setup-Linux","sha256_linux":"%s","notes":"e2e"}`, hex.EncodeToString(sum[:]))
		case "Aegis-Suite-Setup-Linux":
			_, _ = w.Write(installer)
		default:
			http.NotFound(w, r)
		}
	}))
	defer feed.Close()
	// findUpdate() looks beside the test binary; that folder has no
	// staged update, so the check must fall through to the feed.
	cfg := &Config{UpdateFeed: feed.URL + "/releases/latest/download/latest.json"}
	startUpdateCheck(cfg)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := updateSnapshot()
		if !st.busy && st.checked {
			if !st.haveRemote {
				t.Fatalf("expected a remote update, state: %+v", st)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	startUpdateDownload(cfg)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := updateSnapshot()
		if !st.busy && st.stagedPath != "" {
			b, err := os.ReadFile(st.stagedPath)
			if err != nil || string(b) != string(installer) {
				t.Fatalf("staged file wrong: %v", err)
			}
			_ = os.Remove(st.stagedPath)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("download did not stage: %+v", updateSnapshot())
}
