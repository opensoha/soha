// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.2"
	YAMLSHA256 = "1c13104eb5de3042f35371aa2e5ac86b95eeca37ab577b6cf110213901f9f6ab"
	JSONSHA256 = "861d68557d5c16f4d50bf083c5204d5d6065b6bd2a37560e2931a5106bbfebb1"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
