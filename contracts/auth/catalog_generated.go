// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = "2.0.0"
	PermissionCatalogContentSHA256 = "1fb6e61c2a6ae7d411057ea525f759101a31ef8c0518113fdb8f50c1001b3599"
	PermissionCatalogSHA256 = "8f0259d73f425f6cce057cbaba32369a97474fbaa10f7cded4d13ee6e6a6db98"
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
