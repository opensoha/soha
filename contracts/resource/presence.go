package resource

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const ResourcePresenceProtocol = "resource-presence-v1"

// ResourcePresence is one complete, current, name-filtered metadata collection
// snapshot. An absent object is not inferred from an HTTP error. Collection and
// object versions are opaque and must never be ordered or parsed as numbers.
type ResourcePresence struct {
	Protocol                  string     `json:"protocol"`
	APIVersion                string     `json:"apiVersion"`
	Resource                  string     `json:"resource"`
	Kind                      string     `json:"kind"`
	Namespace                 string     `json:"namespace"`
	Name                      string     `json:"name"`
	CollectionResourceVersion string     `json:"collectionResourceVersion"`
	State                     string     `json:"state"`
	UID                       string     `json:"uid,omitempty"`
	ResourceVersion           string     `json:"resourceVersion,omitempty"`
	DeletionTimestamp         *time.Time `json:"deletionTimestamp,omitempty"`
}

func (p ResourcePresence) ValidateFor(kind, namespace, name string) error {
	definition, ok := LookupKubernetesResourceKind(kind)
	if !ok || !definition.GenericYAML || definition.Kind != kind || definition.Namespaced != (namespace != "") {
		return fmt.Errorf("unsupported resource presence target")
	}
	apiVersion := definition.Version
	if definition.Group != "" {
		apiVersion = definition.Group + "/" + definition.Version
	}
	if !p.matchesTarget(apiVersion, definition.Resource, kind, namespace, name) || !presenceOpaque(name) || !presenceOpaque(p.CollectionResourceVersion) {
		return fmt.Errorf("resource presence does not match the original target or collection snapshot")
	}
	if p.State != "present" && p.State != "absent" {
		return fmt.Errorf("explicit resource presence state is required")
	}
	if p.State == "present" {
		if !presenceOpaque(p.UID) || !presenceOpaque(p.ResourceVersion) {
			return fmt.Errorf("present resource requires an exact UID and version")
		}
	} else if p.UID != "" || p.ResourceVersion != "" || p.DeletionTimestamp != nil {
		return fmt.Errorf("absent resource cannot contain a current object identity")
	}
	return nil
}

func (p ResourcePresence) matchesTarget(apiVersion, resource, kind, namespace, name string) bool {
	return p.Protocol == ResourcePresenceProtocol && p.APIVersion == apiVersion && p.Resource == resource && p.Kind == kind && p.Namespace == namespace && p.Name == name
}

func presenceOpaque(value string) bool {
	return value != "" && len(value) <= 256 && strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}
