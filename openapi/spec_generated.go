// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "e5db356d4f7fe79ebafe7a1f6abae0c3cdad90ec703b0c97ec7d343871a7d847"
	JSONSHA256 = "8f0f432f1de5ae1c3d5f61f8b3c57767e621997c6f3a775dc331445c5c58b254"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
