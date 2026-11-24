// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "b0827fdefbc05565f0ddad092eeb6935ac2816bd79ad41d53f39777c1e4538b2"
	JSONSHA256 = "8d37024c04f52b691e26191e0500587fb47677c9a06583f7bae032832a2f4f9c"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
