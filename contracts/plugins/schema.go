package plugins

import _ "embed"

//go:embed plugin-manifest.schema.json
var manifestSchema []byte

//go:embed marketplace-catalog.schema.json
var marketplaceCatalogSchema []byte

//go:embed marketplace-repositories.schema.json
var marketplaceRepositoriesSchema []byte

// ManifestSchema returns the published JSON Schema for distributed plugins.
func ManifestSchema() []byte {
	return append([]byte(nil), manifestSchema...)
}

// MarketplaceCatalogSchema returns the published JSON Schema for marketplace catalogs.
func MarketplaceCatalogSchema() []byte {
	return append([]byte(nil), marketplaceCatalogSchema...)
}

// MarketplaceRepositoriesSchema returns the schema for a repository-only catalog.
func MarketplaceRepositoriesSchema() []byte {
	return append([]byte(nil), marketplaceRepositoriesSchema...)
}
