// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "c84b4ca38a5b26b61c00e5bc7b2a86a5810b0751cc9cea3c882fc870a7a90e5b"
	JSONSHA256 = "7876a147c52c358a2c01418862bde5be91f66c52428b10728ac59830ebda06e4"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
