package plugin

import (
	"context"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type PluginManifest = sohaapi.PluginManifest
type PluginAssetSnapshot = sohaapi.PluginAssetSnapshot
type PluginCapabilityRequest = sohaapi.PluginCapabilityRequest
type PluginCompatibility = sohaapi.PluginCompatibility
type PluginIntegrity = sohaapi.PluginIntegrity
type PluginRuntimeSpec = sohaapi.PluginRuntimeSpec
type PluginExtensionPoints = sohaapi.PluginExtensionPoints
type MarketplacePlugin = sohaapi.MarketplacePlugin
type MarketplacePluginVersion = sohaapi.MarketplacePluginVersion
type InstalledPlugin = sohaapi.InstalledPlugin
type PluginInstallRequest = sohaapi.PluginInstallRequest
type PluginConfigRequest = sohaapi.PluginConfigRequest
type PluginPermissionRequest = sohaapi.PluginPermissionRequest
type MarketplaceCatalog = sohaapi.MarketplaceCatalog

type MarketplaceFilter struct {
	Query          string
	Type           string
	Publisher      string
	SourceID       string
	MarketplaceURL string
	Version        string
}

type PluginVersionRef struct {
	PluginID       string
	Version        string
	SourceID       string
	MarketplaceURL string
}

type ExtensionRecord struct {
	ID             string         `json:"id"`
	PluginID       string         `json:"pluginId"`
	PluginName     string         `json:"pluginName"`
	PluginVersion  string         `json:"pluginVersion"`
	Point          string         `json:"point"`
	Scope          string         `json:"scope"`
	Label          string         `json:"label,omitempty"`
	Description    string         `json:"description,omitempty"`
	ActionRef      string         `json:"actionRef,omitempty"`
	ResourceKinds  []string       `json:"resourceKinds,omitempty"`
	PermissionKeys []string       `json:"permissionKeys,omitempty"`
	RuntimeMode    string         `json:"runtimeMode,omitempty"`
	Status         string         `json:"status"`
	Configured     bool           `json:"configured"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type Repository interface {
	ListInstalled(context.Context) ([]InstalledPlugin, error)
	GetInstalled(context.Context, string) (InstalledPlugin, error)
	UpsertInstalled(context.Context, InstalledPlugin) (InstalledPlugin, error)
	DeleteInstalled(context.Context, string) error
}
