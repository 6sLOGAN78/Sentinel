// Command healthprobe performs a bounded HTTP readiness check for containers.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	probeTimeout          = 3 * time.Second
	maxResponseDrainBytes = 4 << 10
	exitSuccess           = 0
	exitFailure           = 1
	exitUsage             = 2
)

func main() {
	os.Exit(run(os.Args[1:], &http.Client{Timeout: probeTimeout}, os.Stderr))
}

func run(args []string, client *http.Client, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: healthprobe <http-url>")

		return exitUsage
	}

	target, err := url.ParseRequestURI(args[0])
	address := net.ParseIP(targetHostname(target))
	if err != nil || target.Scheme != "http" || target.Host == "" || target.User != nil ||
		address == nil || !address.IsLoopback() {
		_, _ = fmt.Fprintln(stderr, "health probe failed: invalid URL")

		return exitFailure
	}

	// #nosec G704 -- target is restricted above to a literal loopback IP.
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target.String(), nil)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "health probe failed: invalid request")

		return exitFailure
	}

	// #nosec G704 -- target is restricted above to a literal loopback IP.
	response, err := client.Do(request)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "health probe failed: request unsuccessful")

		return exitFailure
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.CopyN(io.Discard, response.Body, maxResponseDrainBytes)

	if response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "health probe failed: status %d\n", response.StatusCode)

		return exitFailure
	}

	return exitSuccess
}

func targetHostname(target *url.URL) string {
	if target == nil {
		return ""
	}

	return target.Hostname()
}
