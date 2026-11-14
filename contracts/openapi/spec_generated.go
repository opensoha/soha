// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "53142496b7e62b324e4be4cf9c8b6836be3cdc540566e40aa37a5599f8f23832"
	JSONSHA256 = "e48e5093cc70ecaff089068975dbf54683cbcbbee95024937f9596e9424cfc99"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
