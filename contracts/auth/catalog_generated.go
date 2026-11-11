// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = "2.0.0"
	PermissionCatalogContentSHA256 = "81015cb50e6b648854ebda71435910439e55c812400b080e2ad3dfe1241069c9"
	PermissionCatalogSHA256 = "fa1fc6bcd68151b761764fecc10e1e5478eff5c1c51c51b4d16863859c0f8b81"
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
