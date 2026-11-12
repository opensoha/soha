// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "ac2411dc6f11f02c6863c3d24ccce6fb63e9e2de534b3a6fc922d5d6b7a27884"
	JSONSHA256 = "44729cda62377f4565dc164a33fe1802d52e5b1a9c2c83a61cf75265dc2e647d"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
