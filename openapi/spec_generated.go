// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "e5b28119b2dca7069009d84f66bc64d0beabf8822d1abf4d3c6350f17dd51579"
	JSONSHA256 = "daf2ff7308a5a2e0c4bd0d9c60eea20b08a2108ebc8e8802d8258bb9e73e8ef2"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
