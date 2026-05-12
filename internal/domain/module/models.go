package module

type Descriptor struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	DefaultPath        string   `json:"defaultPath"`
	EnabledConfigKey   string   `json:"enabledConfigKey"`
	Dependencies       []string `json:"dependencies,omitempty"`
	VisiblePermissions []string `json:"visiblePermissions,omitempty"`
	SeedMenus          []string `json:"seedMenus,omitempty"`
}

type Status struct {
	Descriptor Descriptor `json:"descriptor"`
	Enabled    bool       `json:"enabled"`
}
