// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.2"
	YAMLSHA256 = "361283932b528be4fb4f61a3bcb73fbccd0a56b7ec7154ea0c6af97bcada863c"
	JSONSHA256 = "ea38580f29ba0f4b2bc0c6895a5a8e4f0a957202a84777085af78e527800793a"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
