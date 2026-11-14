package resource

type CRDResourceDefinition struct {
	CRDName    string `json:"crdName"`
	Kind       string `json:"kind"`
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
}
