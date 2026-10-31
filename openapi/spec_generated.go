// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.28"
	YAMLSHA256 = "1dbbc50ffaa41b0ca9c0420914e55ec40866efdaea9ff49233911fbb4a7898a3"
	JSONSHA256 = "b6dc6400c3729c25e919b4559410eb53033560173a5dd94667bd5e6c38f90dae"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
