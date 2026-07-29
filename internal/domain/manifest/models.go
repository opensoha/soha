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
	ID                       string            `json:"id"`
	ApplicationEnvironmentID string            `json:"applicationEnvironmentId"`
	EnvironmentKey           string            `json:"environmentKey"`
	ClusterID                string            `json:"clusterId"`
	Namespace                string            `json:"namespace"`
	Overlay                  map[string]string `json:"overlay,omitempty"`
	Status                   string            `json:"status"`
}

type Package struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	ApplicationID   string    `json:"applicationId"`
	BusinessLineID  string    `json:"businessLineId,omitempty"`
	Renderer        string    `json:"renderer"`
	Status          string    `json:"status"`
	CurrentRevision int       `json:"currentRevision"`
	Files           []File    `json:"files"`
	Bindings        []Binding `json:"bindings"`
	CreatedBy       string    `json:"createdBy,omitempty"`
	UpdatedBy       string    `json:"updatedBy,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Input struct {
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	ApplicationID  string    `json:"applicationId"`
	BusinessLineID string    `json:"businessLineId,omitempty"`
	Renderer       string    `json:"renderer"`
	Files          []File    `json:"files"`
	Bindings       []Binding `json:"bindings"`
}

type Filter struct {
	ApplicationID  string
	ApplicationIDs []string
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
