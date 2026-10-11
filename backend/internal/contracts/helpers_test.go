package contracts_test

import (
	"bytes"
	"fmt"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaBase is the $id prefix of every contract schema.
const schemaBase = "https://sentinel.local/schema/"

// CompileSchema compiles the named embedded schema with format assertions enabled.
func CompileSchema(name string) (*jsonschema.Schema, error) {
	raw, err := contracts.Schema(name)
	if err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse schema %q: %w", name, err)
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()

	url := schemaBase + name
	if err = c.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("add schema resource %q: %w", name, err)
	}

	sch, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile schema %q: %w", name, err)
	}

	return sch, nil
}
