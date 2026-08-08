package observability

import "time"

type Dashboard struct {
	ID                  string
	Name                string
	Source              string
	SourceSchemaVersion int
	DataSourceID        string
	Tags                []string
	Panels              []DashboardPanel
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type DashboardPanel struct {
	ID        string               `json:"id"`
	Title     string               `json:"title"`
	Type      string               `json:"type"`
	Layout    DashboardPanelLayout `json:"layout"`
	Targets   []DashboardTarget    `json:"targets"`
	Queryable bool                 `json:"queryable"`
	Markdown  string               `json:"markdown,omitempty"`
}

type DashboardPanelLayout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type DashboardTarget struct {
	RefID      string `json:"refId"`
	Expression string `json:"expression"`
	Legend     string `json:"legend,omitempty"`
}

type DashboardImportWarning struct {
	Code    string
	Message string
	PanelID string
}

type DashboardImportResult struct {
	Dashboard          Dashboard
	Warnings           []DashboardImportWarning
	ImportedPanelCount int
	SkippedPanelCount  int
}
