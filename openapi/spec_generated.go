// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "2b7ae62c2a6928980fa165c7aefcc1a66bb82b05208eb85ad2f8811370cc4e07"
	JSONSHA256 = "a1f3da34b5dabab7332aa0566d4990173d9d56f04701491f0b4f7d765c1abd95"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
