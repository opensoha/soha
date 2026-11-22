// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.2"
	YAMLSHA256 = "36637d86ac58b32b8c622724567c8c03f712bca1fe617df2d211341dc249d4b9"
	JSONSHA256 = "f47d943c7a78426d696cbf37806a45a0fee1de742b1af337ceb9a0373e004e94"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
