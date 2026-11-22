// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "98dd5bcfd70879e6246899069c578890010a71e1430a170f44442e86dde9b7e7"
	JSONSHA256 = "d772fde3a3c3b8674c5727ab59848e912026c4517abf3e9d3ed3a3090830fbae"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
