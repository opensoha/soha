// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = "2.0.0"
	PermissionCatalogContentSHA256 = "d233e7fcdb01c2776fabc647c8fa39b70e677581ee8219fb3edf57f52d99f55a"
	PermissionCatalogSHA256 = "e5b13f329e53ebfe98b69d5282b960ccbf8094773acd4428a94303e9de5cae07"
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
