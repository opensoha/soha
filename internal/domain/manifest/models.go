package manifest

import (
	"context"
	"time"
)

const (
	StatusDraft     = "draft"
	StatusPublished = "published"

	RendererRaw       = "raw_yaml"
	RendererKustomize = "kustomize"
)

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Binding struct {
	TemplateParameters       map[string]any    `json:"templateParameters,omitempty"`
	ID                       string            `json:"id"`
	ApplicationEnvironmentID string            `json:"applicationEnvironmentId"`
	EnvironmentKey           string            `json:"environmentKey"`
	ClusterID                string            `json:"clusterId"`
	Namespace                string            `json:"namespace"`
	Overlay                  map[string]string `json:"overlay,omitempty"`
	Kustomize                *KustomizeOptions `json:"kustomize,omitempty"`
	Status                   string            `json:"status"`
}

type Package struct {
	ExpectedUpdatedAt *time.Time `json:"-"`
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Description       string     `json:"description,omitempty"`
	ApplicationID     string     `json:"applicationId"`
	ServiceID         string     `json:"serviceId,omitempty"`
	BusinessLineID    string     `json:"businessLineId,omitempty"`
	Renderer          string     `json:"renderer"`
	Status            string     `json:"status"`
	CurrentRevision   int        `json:"currentRevision"`
	Files             []File     `json:"files"`
	Bindings          []Binding  `json:"bindings"`
	CreatedBy         string     `json:"createdBy,omitempty"`
	UpdatedBy         string     `json:"updatedBy,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type Input struct {
	ExpectedUpdatedAt *time.Time `json:"expectedUpdatedAt,omitempty"`
	Name              string     `json:"name"`
	Description       string     `json:"description,omitempty"`
	ApplicationID     string     `json:"applicationId"`
	ServiceID         string     `json:"serviceId,omitempty"`
	BusinessLineID    string     `json:"businessLineId,omitempty"`
	Renderer          string     `json:"renderer"`
	Files             []File     `json:"files"`
	Bindings          []Binding  `json:"bindings"`
}

type Filter struct {
	ApplicationID  string
	ApplicationIDs []string
	ServiceID      string
	ClusterID      string
	Namespace      string
	Search         string
	Page           int
	PageSize       int
	Limit          int
}

type Page struct {
	Items    []Package `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

type RevisionInput struct {
	ExpectedUpdatedAt time.Time `json:"expectedUpdatedAt"`
	Note              string    `json:"note,omitempty"`
}

type Revision struct {
	ID        string    `json:"id"`
	PackageID string    `json:"packageId"`
	Version   int       `json:"version"`
	Digest    string    `json:"digest"`
	Note      string    `json:"note,omitempty"`
	Files     []File    `json:"files"`
	Bindings  []Binding `json:"bindings"`
	CreatedBy string    `json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Repository interface {
	List(context.Context, Filter) (Page, error)
	Get(context.Context, string) (Package, error)
	Create(context.Context, Package) (Package, error)
	Update(context.Context, string, Package) (Package, error)
	Delete(context.Context, string) error
	Publish(context.Context, Package, Revision) (Package, error)
	ListRevisions(context.Context, string) ([]Revision, error)
}
