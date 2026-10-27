// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.25"
	YAMLSHA256 = "696332358715dfbf166d75551c663d6e7018689888d7c5d420b80623e09eb1ae"
	JSONSHA256 = "0b76d83aace5b2463252e8a8a1f43226d038b8bed0a770eab5c5461ddcb156d5"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
