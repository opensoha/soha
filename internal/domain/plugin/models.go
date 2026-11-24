package plugin

import (
	"context"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
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
type PluginSourceInfo = sohaapi.PluginSourceInfo
type PluginRepositoryPreviewRequest = sohaapi.PluginRepositoryPreviewRequest
type PluginRepositoryPreview = sohaapi.PluginRepositoryPreview
type PluginRepositoryPreviewSource = sohaapi.PluginRepositoryPreviewSource
type PluginRepositoryInstallRequest = sohaapi.PluginRepositoryInstallRequest
type PluginRepositoryUpgradeRequest = sohaapi.PluginRepositoryUpgradeRequest
type PluginRepositoryRollbackRequest = sohaapi.PluginRepositoryRollbackRequest
type PluginRepositoryRefsRequest = sohaapi.PluginRepositoryRefsRequest
type PluginRepositoryRef = sohaapi.PluginRepositoryRef
type PluginRepositoryRefList = sohaapi.PluginRepositoryRefList
type PluginUpdate = sohaapi.PluginUpdate
type PluginInstallRequest = sohaapi.PluginInstallRequest
type PluginConfigRequest = sohaapi.PluginConfigRequest
type PluginPermissionRequest = sohaapi.PluginPermissionRequest
type MarketplaceCatalog = sohaapi.MarketplaceCatalog
type PluginPackageDescriptor = sohaapi.PluginPackageDescriptor
type PluginArtifactRecord = sohaapi.PluginArtifactRecord
type CompanionActivationRequest = sohaapi.CompanionActivationRequest
type CompanionRollbackRequest = sohaapi.CompanionRollbackRequest
type CompanionPackManifest = sohaapi.CompanionPackManifest

const (
	SourceKindGitHubRepo     = sohaapi.GithubRepo
	SourceKindMarketplace    = sohaapi.Marketplace
	SourceKindDirectManifest = sohaapi.DirectManifest
)

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
	CreateInstalled(context.Context, InstalledPlugin, func(context.Context) error) (InstalledPlugin, bool, error)
	UpdateInstalled(ctx context.Context, expected InstalledPlugin, item InstalledPlugin, beforeUpdate func(context.Context, *InstalledPlugin) error) (InstalledPlugin, error)
	RemoveInstalled(context.Context, string, func(context.Context, InstalledPlugin) error) (InstalledPlugin, error)
}
