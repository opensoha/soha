package menu

import "context"

type Record struct {
	ID        string   `json:"id"`
	ParentID  string   `json:"parentId,omitempty"`
	Path      string   `json:"path"`
	LabelZH   string   `json:"labelZh"`
	LabelEN   string   `json:"labelEn"`
	IconKey   string   `json:"iconKey"`
	Section   string   `json:"section"`
	SortOrder int      `json:"sortOrder"`
	Enabled   bool     `json:"enabled"`
	RoleIDs   []string `json:"roleIds,omitempty"`
	Children  []Record `json:"children,omitempty"`
}

type Input struct {
	ID        string   `json:"id"`
	ParentID  string   `json:"parentId,omitempty"`
	Path      string   `json:"path"`
	LabelZH   string   `json:"labelZh"`
	LabelEN   string   `json:"labelEn"`
	IconKey   string   `json:"iconKey"`
	Section   string   `json:"section"`
	SortOrder int      `json:"sortOrder"`
	Enabled   bool     `json:"enabled"`
	RoleIDs   []string `json:"roleIds"`
}

type Repository interface {
	List(context.Context) ([]Record, error)
	Get(context.Context, string) (Record, error)
	Create(context.Context, Record) (Record, error)
	Update(context.Context, string, Record) (Record, error)
	Delete(context.Context, string) error
}
