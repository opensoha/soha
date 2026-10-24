// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.26"
	YAMLSHA256 = "ff8d09f65292d9e879191b505dfea2d15d321369936b45ddb1d822fb01047015"
	JSONSHA256 = "fcae19aa1883712f3582c1023943c58ceb8db16bafc4e8bb6b75f9d095cd067d"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
