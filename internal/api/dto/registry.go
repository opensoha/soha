package dto

type UpsertRegistryConnectionRequest struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	RegistryType string         `json:"registryType"`
	Endpoint     string         `json:"endpoint"`
	Namespace    string         `json:"namespace"`
	Username     string         `json:"username"`
	Secret       string         `json:"secret"`
	Insecure     bool           `json:"insecure"`
	Metadata     map[string]any `json:"metadata"`
}
