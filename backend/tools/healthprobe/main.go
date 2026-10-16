// Command healthprobe performs a bounded HTTP readiness check for containers.
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const probeTimeout = 3 * time.Second

func main() {
	os.Exit(run(os.Args[1:], &http.Client{Timeout: probeTimeout}, os.Stderr))
}

func run(args []string, client *http.Client, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: healthprobe <http-url>")

		return 2
	}

	target, err := url.ParseRequestURI(args[0])
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		fmt.Fprintln(stderr, "health probe failed: invalid URL")

		return 1
	}

	request, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		fmt.Fprintln(stderr, "health probe failed: invalid request")

		return 1
	}

	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintln(stderr, "health probe failed: request unsuccessful")

		return 1
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 4<<10)

	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "health probe failed: status %d\n", response.StatusCode)

		return 1
	}

	return 0
}
