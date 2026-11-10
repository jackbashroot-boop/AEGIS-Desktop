package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.7.0", "1.6.0", true},
		{"1.6.0", "1.7.0", false},
		{"1.6.0", "1.6.0", false},
		{"2.0.0", "1.99.99", true},
		{"v1.8.1", "1.8.0", true},
		{"banana", "1.0.0", false},
		{"1.0.0", "banana", false},
		{"1.0", "0.9.9", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// v1.7.1: the repo-feed check parses latest.json over HTTP and the
// download URL resolves next to the feed when no explicit url is set.
func TestFetchLatestAndInstallerURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q, want Bearer test-token", got)
		}
		fmt.Fprint(w, `{"version":"9.9.9","file":"Aegis-Suite-Setup-Linux","sha256":"abc","notes":"n"}`)
	}))
	defer srv.Close()
	cfg := &Config{UpdateFeed: srv.URL + "/releases/latest/download/latest.json", GitHubToken: "test-token"}
	latest, err := fetchLatest(cfg)
	if err != nil {
		t.Fatalf("fetchLatest: %v", err)
	}
	if latest.Version != "9.9.9" || latest.SHA256 != "abc" {
		t.Fatalf("latest = %+v", latest)
	}
	u, err := installerURL(cfg, latest)
	if err != nil || u != srv.URL+"/releases/latest/download/Aegis-Suite-Setup-Linux" {
		t.Fatalf("installerURL = %q, %v", u, err)
	}
	if !versionNewer(latest.Version, appVersion) {
		t.Fatal("9.9.9 must be newer than the app version")
	}
}

func TestFetchLatest404MentionsPrivateRepo(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := fetchLatest(&Config{UpdateFeed: srv.URL + "/latest.json"})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("err = %v, want the private-repo hint", err)
	}
}

func TestLooksLikeFailure(t *testing.T) {
	for _, m := range []string{"Import failed: boom", "Could not write desktop.txt: x", "Port must be a number", "Update download failed: SHA-256 mismatch"} {
		if !looksLikeFailure(m) {
			t.Errorf("want failure: %q", m)
		}
	}
	for _, m := range []string{"Imported 3 file(s) from the card", "Listening on :8791 now", "Update v1.7.2 is available"} {
		if looksLikeFailure(m) {
			t.Errorf("want non-failure: %q", m)
		}
	}
}
