package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type recordingTransport func(*http.Request) (*http.Response, error)

func (f recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRunRejectsRedirects(t *testing.T) {
	t.Parallel()

	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, location := range []string{"http://192.0.2.1/private", "http://127.0.0.1:1/other"} {
			t.Run(strconv.Itoa(status)+location, func(t *testing.T) {
				t.Parallel()

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, location, status)
				}))
				t.Cleanup(server.Close)
				requests := 0
				transport := recordingTransport(func(r *http.Request) (*http.Response, error) {
					requests++
					if r.URL.String() == server.URL {
						return http.DefaultTransport.RoundTrip(r)
					}

					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("ready")),
						Request:    r,
					}, nil
				})
				client := &http.Client{Timeout: time.Second, Transport: transport}
				var stderr bytes.Buffer
				if got := run([]string{server.URL}, client, &stderr); got != exitFailure {
					t.Errorf("redirect returned %d, want failure", got)
				}
				if requests != 1 {
					t.Errorf("made %d requests, want exactly one", requests)
				}
				if client.CheckRedirect != nil {
					t.Error("run mutated supplied client")
				}
			})
		}
	}
}

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
