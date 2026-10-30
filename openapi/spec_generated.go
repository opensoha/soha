// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version    = "0.1.26"
	YAMLSHA256 = "93f20121144b8c086b9ee6e299f778bf6a93444780fea9cce356652adbc291b9"
	JSONSHA256 = "6cbf4c01e7e0791f75276ca79d320a948c6d42e9944abdb2c759a43c6078b67d"
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
