package network

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmbeddedSchemasExposeCanonicalFiles(t *testing.T) {
	for name, schema := range map[string][]byte{
		"runtime": RuntimeProtocolSchema(),
		"ingest":  IngestEventSchema(),
		"radius":  RadiusAccountingSchema(),
	} {
		if !json.Valid(schema) {
			t.Fatalf("%s schema is not valid JSON", name)
		}
		if !strings.Contains(string(schema), "network-") {
			t.Fatalf("%s schema does not contain its versioned contract", name)
		}
	}
}
