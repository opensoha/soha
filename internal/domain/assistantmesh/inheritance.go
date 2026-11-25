package assistantmesh

type InheritanceInput struct {
	OwnerID  string
	HeirID   string
	AssetIDs []string
}

type InheritanceAsset struct {
	ID               string `json:"id"`
	OwnerID          string `json:"ownerId"`
	OriginalAuthorID string `json:"originalAuthorId"`
	Kind             string `json:"kind"`
	Title            string `json:"title"`
	Version          int    `json:"version"`
}

type InheritancePage struct {
	Items     []InheritanceAsset `json:"items"`
	NextAfter string             `json:"nextAfter,omitempty"`
}

type InheritanceResult struct {
	Transferred int `json:"transferred"`
}
