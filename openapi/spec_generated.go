// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.25"
	YAMLSHA256 = "ac5e64ae68f8d822c5a38014c72defbb0e689ddab32d9abbac15c52fa1b6d9b9"
	JSONSHA256 = "73198e43cde0079666f83e127c4f348d86edc5607e8623b47b94b2898e61c4fe"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
