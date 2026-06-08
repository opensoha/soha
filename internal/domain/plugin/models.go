package plugin

import (
	"context"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type PluginManifest = sohaapi.PluginManifest
type PluginAssetSnapshot = sohaapi.PluginAssetSnapshot
type PluginCapabilityRequest = sohaapi.PluginCapabilityRequest
type PluginIntegrity = sohaapi.PluginIntegrity
type MarketplacePlugin = sohaapi.MarketplacePlugin
type InstalledPlugin = sohaapi.InstalledPlugin
type PluginInstallRequest = sohaapi.PluginInstallRequest
type PluginConfigRequest = sohaapi.PluginConfigRequest
type PluginPermissionRequest = sohaapi.PluginPermissionRequest

type MarketplaceFilter struct {
	Query     string
	Type      string
	Publisher string
}

type Repository interface {
	ListInstalled(context.Context) ([]InstalledPlugin, error)
	GetInstalled(context.Context, string) (InstalledPlugin, error)
	UpsertInstalled(context.Context, InstalledPlugin) (InstalledPlugin, error)
	DeleteInstalled(context.Context, string) error
}
