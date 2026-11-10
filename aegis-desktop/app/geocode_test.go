package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeZip(t *testing.T) {
	for in, want := range map[string]string{"06360": "06360", " 06360-1234 ": "06360"} {
		got, err := normalizeZip(in)
		if err != nil || got != want {
			t.Fatalf("normalizeZip(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "6360", "0636O", "063600", "-----"} {
		if _, err := normalizeZip(bad); err == nil {
			t.Fatalf("normalizeZip(%q) accepted", bad)
		}
	}
}

func TestGeocodeZip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("postalcode"); got != "06360" {
			t.Errorf("postalcode param = %q", got)
		}
		fmt.Fprint(w, `[{"lat":"41.5247","lon":"-72.0759","display_name":"Norwich, Connecticut, 06360, United States"}]`)
	}))
	defer srv.Close()
	old := nominatimBase
	nominatimBase = srv.URL
	defer func() { nominatimBase = old }()
	lat, lon, place, err := geocodeZip(context.Background(), "06360")
	if err != nil {
		t.Fatal(err)
	}
	if lat != 41.5247 || lon != -72.0759 || place == "" {
		t.Fatalf("geocode = %v, %v, %q", lat, lon, place)
	}
}

func TestGeocodeZipNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	old := nominatimBase
	nominatimBase = srv.URL
	defer func() { nominatimBase = old }()
	if _, _, _, err := geocodeZip(context.Background(), "00000"); err == nil {
		t.Fatal("empty result accepted")
	}
}
