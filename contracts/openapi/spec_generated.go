// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.0"
	YAMLSHA256 = "35cf75a5b8bfe4c47286dbeb7d1f6e09d4df361640cc5153a41b90878fdf59e4"
	JSONSHA256 = "987f00e101d089c808ac28da3871940bf7e5b2976a335b7dcb1cdbaa4819153f"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
