// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "8bc7c8e725fbafc4620d7a8bf7ef3f1dda9734b09bd975623e00dd0df711447c"
	JSONSHA256 = "017d262264293b61f9eb3444c4ea3c864813e3a5312d637bf6fa0e3e3a1292c8"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
