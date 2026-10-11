// Package contracts defines the versioned Sentinel event and alert contracts.
package contracts

import (
	"embed"
	"fmt"
)

// schemaFS holds the JSON Schema documents.
//
//go:embed schema/*.json
var schemaFS embed.FS //nolint:gochecknoglobals // go:embed requires a package-level variable

// Schema returns the raw bytes of the named schema file, for example "event.v1.json".
func Schema(name string) ([]byte, error) {
	b, err := schemaFS.ReadFile("schema/" + name)
	if err != nil {
		return nil, fmt.Errorf("read schema %q: %w", name, err)
	}

	return b, nil
}
