package copilot

const (
	RuntimeScopeChat = "chat"
	RuntimeScopeHeya = "heya"
)

type RuntimeAssetRef struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Version  string `json:"version"`
	Required bool   `json:"required"`
}

type RuntimeProfile struct {
	ID               string                    `json:"id"`
	Revision         int64                     `json:"revision"`
	Name             string                    `json:"name"`
	Scope            string                    `json:"scope"`
	Enabled          bool                      `json:"enabled"`
	Assets           []RuntimeAssetRef         `json:"assets"`
	ModelPreferences WorkbenchModelPreferences `json:"modelPreferences,omitzero"`
	MaxOutputTokens  int                       `json:"maxOutputTokens,omitempty"`
}
