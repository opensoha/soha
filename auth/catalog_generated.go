// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = "2.0.0"
	PermissionCatalogContentSHA256 = "bd4a7240d34750d3fcf38b4649f6dbe67b17fb0f6ebeb96a32f8d874685e5150"
	PermissionCatalogSHA256 = "835ccdfccfa955ae3ee9377fffb4c523b53d2eb647d01840f79cce5524d72c36"
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
