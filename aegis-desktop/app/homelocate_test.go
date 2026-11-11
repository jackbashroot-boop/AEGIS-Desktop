package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchIPLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":41.66,"longitude":-72.69,"city":"New London","region":"Connecticut","country_name":"United States"}`))
	}))
	defer srv.Close()
	old := ipLocBase
	ipLocBase = srv.URL
	defer func() { ipLocBase = old }()

	loc, err := fetchIPLocation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loc.Lat != 41.66 || loc.Lon != -72.69 || loc.Place != "New London, Connecticut" {
		t.Fatalf("got %+v", loc)
	}
}

func TestFetchIPLocationDeclined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":true,"reason":"RateLimited"}`))
	}))
	defer srv.Close()
	old := ipLocBase
	ipLocBase = srv.URL
	defer func() { ipLocBase = old }()
	if _, err := fetchIPLocation(context.Background()); err == nil {
		t.Fatal("expected an error for a declined lookup")
	}
}
