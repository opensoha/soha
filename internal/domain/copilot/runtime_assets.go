package copilot

type RuntimeAssetDescriptor struct {
	Kind              string `json:"kind"`
	ID                string `json:"id"`
	Version           string `json:"version"`
	Transport         string `json:"transport,omitempty"`
	ExecutionPosition string `json:"executionPosition,omitempty"`
}

func (a RuntimeAssetDescriptor) Ref() RuntimeAssetRef {
	return RuntimeAssetRef{Kind: a.Kind, ID: a.ID, Version: a.Version}
}

type RuntimeAsset struct {
	RuntimeAssetDescriptor
	Enabled bool `json:"enabled"`
}

const (
	RuntimeMCPProbeReady          = "ready"
	RuntimeMCPProbeUnavailable    = "unavailable"
	RuntimeMCPProbeCleanupUnknown = "cleanup_unknown"
)

type RuntimeMCPProbe struct {
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}
