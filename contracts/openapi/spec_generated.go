// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "cb8195db4a75b88d8df858142955c209c90b3aa9abac0edaf6998a58bbbc46d8"
	JSONSHA256 = "b9fc58982b3c775e9345a0cdb4c2dead781bdea8b4beaf13f4c34ef9748bf35b"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
