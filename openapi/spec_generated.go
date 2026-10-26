// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.26"
	YAMLSHA256 = "6e40cb5fdf9f94ae11dd0698eef427ae98bf354a148ea3214e31aa0a93fcb7b9"
	JSONSHA256 = "2decfb93945c34a2412763d04a3c6766f351fd6bd7664393f93b16b040decd64"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
