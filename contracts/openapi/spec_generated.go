// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.2"
	YAMLSHA256 = "2a2738b61560523e0eeded88ce243f64a05cb9bd9d2a70aa5f0b475b0f2b6dc7"
	JSONSHA256 = "b9960866705d35548f443f94f8d2b1350a719df129b8af986701bae43227d731"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
