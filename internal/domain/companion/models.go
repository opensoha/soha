package companion

import (
	"context"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const (
	BuiltinPluginID = "builtin.soha-companion"
	BuiltinVersion  = "builtin"
)

type Profile = sohaapi.CompanionProfile
type ProfileResetRequest = sohaapi.CompanionProfileResetRequest
type InteractionRequest = sohaapi.CompanionInteractionRequest
type InteractionReceipt = sohaapi.CompanionInteractionReceipt
type InteractionDefinition = sohaapi.CompanionInteractionDefinition
type UnlockDefinition = sohaapi.CompanionUnlockDefinition
type PackManifest = sohaapi.CompanionPackManifest
type Asset = sohaapi.CompanionAsset
type PackageDescriptor = sohaapi.PluginPackageDescriptor
type ArtifactRecord = sohaapi.PluginArtifactRecord

type Artifact struct {
	PluginID         string
	Version          string
	SHA256           string
	SizeBytes        int64
	ChecksumStatus   string
	SignatureStatus  string
	ProvenanceStatus string
	StorageDigest    string
	Assets           []Asset
	Pack             PackManifest
	Active           bool
	InstalledAt      time.Time
	RetiredAt        *time.Time
}

type ApplyInteraction struct {
	OwnerID        string
	IdempotencyKey string
	InputHash      string
	PluginID       string
	Version        string
	InteractionID  string
	Cooldown       time.Duration
	ClientRevision int64
	RewardXP       int
	RewardAffinity int
	Unlocks        []UnlockDefinition
	Now            time.Time
}

type ResetProfile struct {
	OwnerID        string
	IdempotencyKey string
	InputHash      string
	PluginID       string
	Version        string
	Now            time.Time
}

type Repository interface {
	EnsureProfile(context.Context, string, time.Time) (Profile, error)
	ApplyInteraction(context.Context, ApplyInteraction) (InteractionReceipt, bool, error)
	ResetProfile(context.Context, ResetProfile) (Profile, bool, error)
	PutArtifact(context.Context, Artifact) (Artifact, error)
	ListArtifacts(context.Context, string) ([]Artifact, error)
	ActivateArtifact(context.Context, string, string, time.Time) (Artifact, error)
	GetActiveArtifact(context.Context, string) (Artifact, error)
	RetireArtifacts(context.Context, string, time.Time) error
}
