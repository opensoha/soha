// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "6cb366cfeb66389b7e391aded4d4a70e5e66e07f625b6703a52f00bcafaa01af"
	JSONSHA256 = "fb4a10580eb6476c272d1bcbc9516c07832f0c5fe209602904b494ee2e95fdc2"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
