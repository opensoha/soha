package contentdlp

type ShadowInput struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	Samples          [][]byte `json:"samples"`
}
type ShadowDecision struct {
	StepUpRequired   bool     `json:"stepUpRequired,omitempty"`
	Enabled          bool     `json:"enabled"`
	Mode             string   `json:"mode"`
	Status           string   `json:"status"`
	Allowed          bool     `json:"allowed"`
	DetectorVersion  string   `json:"detectorVersion"`
	MatchedRuleIDs   []string `json:"matchedRuleIds"`
	DataLevel        string   `json:"dataLevel"`
	MinimumDataLevel string   `json:"minimumDataLevel"`
	RuleSetVersion   string   `json:"ruleSetVersion"`
}
type ShadowSample struct {
	Index     int             `json:"index"`
	SizeBytes int64           `json:"sizeBytes"`
	Draft     ShadowDecision  `json:"draft"`
	Published *ShadowDecision `json:"published,omitempty"`
}
type ShadowResult struct {
	Revision            int64          `json:"revision"`
	PublishedVersion    int64          `json:"publishedVersion"`
	DraftRuleSetVersion string         `json:"draftRuleSetVersion"`
	Samples             []ShadowSample `json:"samples"`
	WouldBlock          int            `json:"wouldBlock"`
	WouldWarn           int            `json:"wouldWarn"`
	Uninspected         int            `json:"uninspected"`
	Changed             int            `json:"changed"`
}
