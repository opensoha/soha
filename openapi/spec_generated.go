// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.28"
	YAMLSHA256 = "c7ff49d1d8ec98ae922fb7d82bdb67e17b069aea29fa16b8532cbefe933b4bbf"
	JSONSHA256 = "94a97a39ed3023b7fb8e72dbf2d23c25cddf80643d815db0341256fcf3c07805"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
