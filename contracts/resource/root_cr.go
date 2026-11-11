package resource

import (
	"fmt"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
)

// ValidateRootCRDocument prevents a root publication from changing runtime metadata
// or writing generated children. Operator reconciliation remains independent.
func ValidateRootCRDocument(object map[string]any, root sohaapi.ManifestRootResource) error {
	metadata, ok := object["metadata"].(map[string]any)
	if !ok {
		return fmt.Errorf("root CR metadata is required")
	}
	if root.UID == "" || root.Generation < 1 || root.CrdName == "" || !strings.Contains(root.APIVersion, "/") || root.Namespace == "" {
		return fmt.Errorf("root CR requires a frozen namespaced identity")
	}
	if object["apiVersion"] != root.APIVersion || object["kind"] != root.Kind || metadata["namespace"] != root.Namespace || metadata["name"] != root.Name {
		return fmt.Errorf("root CR identity differs from the approved resource")
	}
	for key := range object {
		switch key {
		case "apiVersion", "kind", "metadata", "spec":
		default:
			return fmt.Errorf("root CR updates only support spec")
		}
	}
	for key := range metadata {
		if key != "name" && key != "namespace" {
			return fmt.Errorf("root CR runtime metadata cannot be published")
		}
	}
	if _, ok := object["spec"].(map[string]any); !ok {
		return fmt.Errorf("root CR spec must be an object")
	}
	return nil
}
