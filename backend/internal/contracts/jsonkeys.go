package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// maxJSONDepth bounds object/array nesting during the key scan. Valid events
// and alerts nest at most two levels, so this only stops abusive input.
const maxJSONDepth = 32

// rootKeys walks raw token by token and returns the root object's keys. It
// rejects a non-object root and any object, at any depth, that repeats a key:
// encoding/json keeps the last duplicate while other parsers may keep the
// first, so duplicates would let tenant_id or event_id mean different things
// to different consumers.
func rootKeys(raw []byte) (map[string]struct{}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("json token: %w", err)
	}

	if tok != json.Delim('{') {
		return nil, fieldErr("$", "must be a JSON object")
	}

	return scanObject(dec, "", 1)
}

// scanObject consumes the members of an object whose '{' was already read
// and returns its keys. path prefixes nested field names in errors.
func scanObject(dec *json.Decoder, path string, depth int) (map[string]struct{}, error) {
	seen := map[string]struct{}{}

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("json token: %w", err)
		}

		key, ok := tok.(string)
		if !ok {
			return nil, fieldErr(path+"$", "object key is not a string")
		}

		field := path + key
		if _, dup := seen[key]; dup {
			return nil, fieldErr(field, "duplicate field")
		}

		seen[key] = struct{}{}

		if err = scanValue(dec, field+".", depth); err != nil {
			return nil, err
		}
	}

	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, fmt.Errorf("json token: %w", err)
	}

	return seen, nil
}

// scanValue consumes one value, descending into objects and arrays.
func scanValue(dec *json.Decoder, path string, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("json token: %w", err)
	}

	delim, ok := tok.(json.Delim)
	if !ok || delim == '}' || delim == ']' {
		return nil
	}

	if depth >= maxJSONDepth {
		return fieldErr(path+"$", "nesting exceeds %d levels", maxJSONDepth)
	}

	if delim == '{' {
		_, err = scanObject(dec, path, depth+1)

		return err
	}

	for dec.More() {
		if err = scanValue(dec, path, depth+1); err != nil {
			return err
		}
	}

	if _, err = dec.Token(); err != nil { // closing ']'
		return fmt.Errorf("json token: %w", err)
	}

	return nil
}
