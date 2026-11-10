package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestFwFeedAndStage(t *testing.T) {
	bin := []byte("FAKE-FIRMWARE-IMAGE")
	sum := sha256.Sum256(bin)
	sumHex := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/rel/phoenix-latest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":"9.9.9","file":"aegis-phoenix-adv.bin","sha256":"%s","size":%d}`, sumHex, len(bin))
	})
	mux.HandleFunc("/rel/aegis-phoenix-adv.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(bin)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := &Config{Library: t.TempDir(), UpdateFeed: srv.URL + "/rel/latest.json"}
	info, err := fetchFwFeed(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "9.9.9" || info.BinURL != srv.URL+"/rel/aegis-phoenix-adv.bin" {
		t.Fatalf("feed = %+v", info)
	}
	if err := downloadFwBin(cfg, info); err != nil {
		t.Fatal(err)
	}
	st, p, ok := stagedFw(cfg)
	if !ok || st.Version != "9.9.9" || filepath.Base(p) != "aegis-phoenix-adv.bin" {
		t.Fatalf("staged = %+v %q %v", st, p, ok)
	}
	// corrupt metadata size -> no longer offerable
	if !fwNewer("0.12.4", "0.12.3") || fwNewer("0.12.3", "0.12.4") || fwNewer("1.0.0", "1.0.0") {
		t.Fatal("fwNewer broken")
	}
	_ = srv
}
