// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "8d2599ab9a0f09c8623d9389f6720e4599e0e6d26c35e8c34ef51d01539f968f"
	JSONSHA256 = "10f4fa84c769fd2f510423e12afbbd80edc6626137e726e2d7b5013df2c5f77c"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
