// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.2.3"
	YAMLSHA256 = "7e58852747d6f7e8722885258984d4b7097fde8083c93cf27766438af675b0a5"
	JSONSHA256 = "2cd5266be4aa99e94f9c64ed681a3b28120ead41cd9133c5575cfa1c702ec669"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
