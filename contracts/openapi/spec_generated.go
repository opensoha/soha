// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "710c5e0c49149d6e7a643c70b8a3578f47c70e0ef6a2b63e3960ec8c9c7f07db"
	JSONSHA256 = "05a2d5f3c6a29bd8d7742171f77c2805475388ff694d8489711d1432275b6d89"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
