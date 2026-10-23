package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("unexpected probe path: %s", r.URL.Path)
				}
				w.Header().Set("Location", "/healthz")
				w.WriteHeader(status)
			}))
			defer server.Close()
			address := strings.TrimPrefix(server.URL, "http://")
			if err := healthcheck(address); (err == nil) != (status == http.StatusOK) {
				t.Fatalf("healthcheck status %d: %v", status, err)
			}
			if status == http.StatusOK {
				_, port, _ := strings.Cut(address, ":")
				if err := healthcheck(":" + port); err != nil {
					t.Fatalf("wildcard listen address: %v", err)
				}
			}
		})
	}
}

func TestHealthcheckRejectsInvalidAddress(t *testing.T) {
	if err := healthcheck("invalid"); err == nil {
		t.Fatal("expected invalid listen address to fail")
	}
}
