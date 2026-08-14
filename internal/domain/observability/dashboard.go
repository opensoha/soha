package observability

import "time"

type Dashboard struct {
	ID                  string
	Name                string
	Source              string
	SourceFormat        string
	SourceSchemaVersion int
	DataSourceID        string
	Tags                []string
	Panels              []DashboardPanel
	Variables           []DashboardVariable
	DataSourceBindings  []DashboardDataSourceBinding
	ImportWarnings      []DashboardImportWarning
	RawJSON             string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type DashboardPanel struct {
	ID              string               `json:"id"`
	Title           string               `json:"title"`
	Type            string               `json:"type"`
	Layout          DashboardPanelLayout `json:"layout"`
	Targets         []DashboardTarget    `json:"targets"`
	Queryable       bool                 `json:"queryable"`
	Markdown        string               `json:"markdown,omitempty"`
	SourcePanelType string               `json:"sourcePanelType,omitempty"`
	Unsupported     bool                 `json:"unsupported,omitempty"`
	RawJSON         string               `json:"rawJson,omitempty"`
}

type DashboardPanelLayout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type DashboardTarget struct {
	RefID          string `json:"refId"`
	Expression     string `json:"expression"`
	Legend         string `json:"legend,omitempty"`
	DataSourceType string `json:"dataSourceType,omitempty"`
	DataSourceUID  string `json:"dataSourceUid,omitempty"`
	DataSourceID   string `json:"dataSourceId,omitempty"`
}

type DashboardVariable struct {
	Name    string   `json:"name"`
	Label   string   `json:"label,omitempty"`
	Type    string   `json:"type"`
	Current string   `json:"current,omitempty"`
	Options []string `json:"options"`
}

type DashboardDataSourceBinding struct {
	Type         string `json:"type"`
	UID          string `json:"uid,omitempty"`
	DataSourceID string `json:"dataSourceId"`
}

type DashboardImportWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	PanelID string `json:"panelId,omitempty"`
}

type DashboardImportResult struct {
	Dashboard          Dashboard
	Warnings           []DashboardImportWarning
	ImportedPanelCount int
	SkippedPanelCount  int
}
