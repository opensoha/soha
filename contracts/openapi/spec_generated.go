// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "94249ea8be3fa8593223db6118813c36b0c06d470fcbf1c83f142dee11ca0527"
	JSONSHA256 = "4f0b9b1c036bc7b866597f1db23b58f37527cf4f22ac40930a21c7afc758ca9d"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
