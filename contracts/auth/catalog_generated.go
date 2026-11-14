// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = "2.0.0"
	PermissionCatalogContentSHA256 = "132db706644d454f3706998f29f1ca9d7de4fece9a7adaeb00acc6597386104a"
	PermissionCatalogSHA256 = "b1e46497db536a6aca9b909215ba8911ea4a3ed3885f604f238ffc43f3759a8f"
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
