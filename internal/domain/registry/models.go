package registry

import "context"

type Connection struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	RegistryType string         `json:"registryType"`
	Endpoint     string         `json:"endpoint"`
	Namespace    string         `json:"namespace,omitempty"`
	Username     string         `json:"username,omitempty"`
	Secret       string         `json:"secret,omitempty"`
	Insecure     bool           `json:"insecure"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    string         `json:"createdAt"`
	UpdatedAt    string         `json:"updatedAt"`
}

type Input struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	RegistryType string         `json:"registryType"`
	Endpoint     string         `json:"endpoint"`
	Namespace    string         `json:"namespace,omitempty"`
	Username     string         `json:"username,omitempty"`
	Secret       string         `json:"secret,omitempty"`
	Insecure     bool           `json:"insecure"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type Repository interface {
	List(context.Context, int) ([]Connection, error)
	Create(context.Context, Connection) (Connection, error)
	Update(context.Context, string, Connection) (Connection, error)
	Delete(context.Context, string) error
}
