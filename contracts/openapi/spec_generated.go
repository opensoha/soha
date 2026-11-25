// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "e07cebdbe977616c3f2523a598a3563d8ba3c30b494fe90cc9488d05292520cb"
	JSONSHA256 = "7cd85b87e84d317b588b59cb3082f8ae8b0b9d2774be8d15b65653714cf1c298"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
