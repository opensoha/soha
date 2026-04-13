package scopegrant

import (
	"context"
	"time"
)

type Record struct {
	ID             string    `json:"id"`
	SubjectType    string    `json:"subjectType"`
	SubjectID      string    `json:"subjectId"`
	BusinessLineID string    `json:"businessLineId"`
	EnvironmentIDs []string  `json:"environmentIds,omitempty"`
	ApplicationIDs []string  `json:"applicationIds,omitempty"`
	Role           string    `json:"role"`
	Effect         string    `json:"effect"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Input struct {
	ID             string   `json:"id"`
	SubjectType    string   `json:"subjectType"`
	SubjectID      string   `json:"subjectId"`
	BusinessLineID string   `json:"businessLineId"`
	EnvironmentIDs []string `json:"environmentIds,omitempty"`
	ApplicationIDs []string `json:"applicationIds,omitempty"`
	Role           string   `json:"role"`
	Effect         string   `json:"effect"`
	Enabled        bool     `json:"enabled"`
}

type Repository interface {
	List(context.Context) ([]Record, error)
	Get(context.Context, string) (Record, error)
	Create(context.Context, Input) (Record, error)
	Update(context.Context, string, Input) (Record, error)
	Delete(context.Context, string) error
}
