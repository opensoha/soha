// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.1"
	YAMLSHA256 = "b8ecfbbc46b0e199cc329de2b4906f3d07b79ab01433724cb9e19673a2d2087b"
	JSONSHA256 = "79b73d3aff787f0c5d0b90e0da428bf535b9bf2721db8209a75cfc1d8cc73507"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
