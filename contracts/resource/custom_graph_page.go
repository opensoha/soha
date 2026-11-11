package resource

import "encoding/json"

// CustomResourceGraphObjectPath is relative to the authenticated Agent platform API.
const CustomResourceGraphObjectPath = "/extensions/custom-resources/graph-object"

type CustomResourceGraphObjectRequest struct {
	Definition CRDResourceDefinition `json:"definition"`
	Namespace  string                `json:"namespace"`
	Name       string                `json:"name"`
}

// CustomResourceGraphPage contains only observed GVK and relationship metadata.
type CustomResourceGraphPage struct {
	Items     []json.RawMessage `json:"items"`
	Truncated bool              `json:"truncated"`
}
