// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = "2.0.0"
	PermissionCatalogContentSHA256 = "833d71204e5135f6b21dda08214b4ff1226c5d2fee04d017c837935e970f4542"
	PermissionCatalogSHA256 = "0c8de95a29ec13ed5ea622f5e7e8b0e0ab22feb1dc772223f8aff66b7ae5d9a3"
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
