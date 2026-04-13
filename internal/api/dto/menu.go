package dto

type UpsertMenuRequest struct {
	ID        string   `json:"id"`
	ParentID  string   `json:"parentId"`
	Path      string   `json:"path"`
	LabelZH   string   `json:"labelZh"`
	LabelEN   string   `json:"labelEn"`
	IconKey   string   `json:"iconKey"`
	Section   string   `json:"section"`
	SortOrder int      `json:"sortOrder"`
	Enabled   bool     `json:"enabled"`
	RoleIDs   []string `json:"roleIds"`
}
