// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "a71ae00f6b0e6581b68c2400898e01d807932da56de0d2702f4e23e65b0e690b"
	JSONSHA256 = "4128d329e31632b1bcc80c1511b3c9fd65ede1b908251f7cec1c1ec65b7ff20d"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
