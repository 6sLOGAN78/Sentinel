// Command check-alerts reconciles a private current-run manifest.
package main

import (
	"errors"
)

type manifest struct {
	Tenant     string   `json:"tenant"`
	SourceKeys []string `json:"source_keys"`
	EventIDs   []string `json:"event_ids"`
	Expected   []string `json:"expected_alert_bytes"`
}

func reconcile(_ manifest, _ [][]byte, _ [][]byte, _ [][]byte) error {
	return errors.New("current run is not reconciled")
}

func main() {}
