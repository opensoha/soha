// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.26"
	YAMLSHA256 = "691aa93499e5809eec33c3925040d433806c7483e1f5b8b8ba60bae0edd34f34"
	JSONSHA256 = "3bf12be892cf7286d71d64fde8d000a7bf0c1974bdf58d06abba3b10355b4d5e"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
