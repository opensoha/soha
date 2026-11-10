// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "42b253032788191a661673a77380193206941492e34bddbaa9b039851b37e991"
	JSONSHA256 = "34f246ba4e4ec6af0382de74f7120cfb8a3b7f31d084e9f1497a785c38456f67"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
