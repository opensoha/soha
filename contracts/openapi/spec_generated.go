// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "95bcc58f72adcb0aa2f6454c4d6a96ac0cebc7b563c0439c78a7b8737f0531f0"
	JSONSHA256 = "d1b3594a02f6f127870cd8bc641ba44a264857a6f1b458bc05a17641f50fbed9"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
