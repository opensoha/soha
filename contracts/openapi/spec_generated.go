// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "a2787a50ac09a1865d7b81ab8979e5667d679d434ad3dbdfb6af3f9ee3dc5994"
	JSONSHA256 = "2639dec63179dd28968d6b8fff282bfb38bc801eae9a2bb5122832bcf8b78d2f"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
