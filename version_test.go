package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.6.1", "0.6.0", true},
		{"0.10.0", "0.9.2", true},
		{"1.0", "0.99.9", true},
		{"0.6.0", "0.6.0", false},
		{"0.5.9", "0.6.0", false},
		{"0.6.0.1", "0.6.0", true},
		{"", "0.6.0", false},
		{"0.7.0", "", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.7.0","html_url":"https://github.com/amithgc/quarters/releases/tag/v0.7.0"}`))
	}))
	defer srv.Close()
	v, url, err := fetchLatest(srv.URL)
	if err != nil || v != "0.7.0" || url != "https://github.com/amithgc/quarters/releases/tag/v0.7.0" {
		t.Fatalf("fetchLatest = %q, %q, %v", v, url, err)
	}
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "rate limited", http.StatusForbidden) }))
	defer gone.Close()
	if _, _, err := fetchLatest(gone.URL); err == nil {
		t.Fatal("a refused request should be an error")
	}
}
