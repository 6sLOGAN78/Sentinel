package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsSuccessOnlyForHTTP200(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		code int
		want int
	}{
		{name: "ready", code: http.StatusOK, want: 0},
		{name: "unavailable", code: http.StatusServiceUnavailable, want: 1},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
			}))
			t.Cleanup(server.Close)

			var stderr bytes.Buffer
			got := run([]string{server.URL}, &http.Client{Timeout: time.Second}, &stderr)
			if got != tc.want {
				t.Fatalf("run() = %d, want %d; stderr=%q", got, tc.want, stderr.String())
			}
		})
	}
}

func TestRunTimesOut(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	var stderr bytes.Buffer
	got := run([]string{server.URL}, &http.Client{Timeout: 10 * time.Millisecond}, &stderr)
	if got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
	if strings.Contains(stderr.String(), server.URL) {
		t.Fatalf("stderr leaked probe URL: %q", stderr.String())
	}
}

func TestRunRejectsInvalidURL(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if got := run([]string{"not-a-url"}, &http.Client{Timeout: time.Second}, &stderr); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestRunRequiresExactlyOneURL(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"http://one", "http://two"}} {
		var stderr bytes.Buffer
		if got := run(args, &http.Client{Timeout: time.Second}, &stderr); got != 2 {
			t.Fatalf("run(%q) = %d, want 2", args, got)
		}
	}
}
