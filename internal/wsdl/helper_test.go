package wsdl

import (
	"encoding/xml"
	"strings"
	"testing"
)

// parseSchemasForTest extracts the embedded schemas from a WSDL for unit tests.
func parseSchemasForTest(t *testing.T, data string) []schemaElem {
	t.Helper()
	var d defs
	dec := xml.NewDecoder(strings.NewReader(data))
	dec.Strict = false
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("decode schemas: %v", err)
	}
	return d.Schemas
}
