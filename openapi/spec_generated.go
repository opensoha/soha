// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.29"
	YAMLSHA256 = "3dccce8091c423ad2abf84d5aa34650f5001cefa65a0f7edd779478b7a0c907a"
	JSONSHA256 = "bdda1696fec97e8b939a5f9233c89f763d6dd07f93719f3b3e51d313347ce673"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
