package copilot

// AssistantMeshSkill selects an immutable, server-authorized Skill snapshot for a single run.
type AssistantMeshSkill struct {
	AssetID    string         `json:"assetId"`
	Digest     string         `json:"digest"`
	Version    int            `json:"version,omitempty"`
	Trial      bool           `json:"trial"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type AssistantMeshMCP struct {
	AssetID string `json:"assetId"`
	Digest  string `json:"digest"`
}
