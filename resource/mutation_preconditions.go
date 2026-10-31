package resource

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ResourceMutationPreconditions pins the object reviewed by the caller. Values
// are opaque; in particular, resourceVersion must not be parsed as a number.
type ResourceMutationPreconditions struct {
	ExpectedUID             string `json:"expectedUid"`
	ExpectedResourceVersion string `json:"expectedResourceVersion"`
}

func (p ResourceMutationPreconditions) Validate() error {
	for _, value := range []string{p.ExpectedUID, p.ExpectedResourceVersion} {
		if value == "" || len(value) > 256 || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return fmt.Errorf("expectedUid and expectedResourceVersion must be non-empty opaque values of at most 256 bytes without whitespace or control characters")
		}
	}
	return nil
}

// ResourceIdentity deliberately contains no resource content or Secret data.
type ResourceIdentity struct {
	APIVersion        string     `json:"apiVersion"`
	Kind              string     `json:"kind"`
	Name              string     `json:"name"`
	Namespace         string     `json:"namespace,omitempty"`
	UID               string     `json:"uid"`
	ResourceVersion   string     `json:"resourceVersion"`
	DeletionTimestamp *time.Time `json:"deletionTimestamp,omitempty"`
}

type ConditionalResourceYAMLInput struct {
	Content       string                        `json:"content"`
	Preconditions ResourceMutationPreconditions `json:"preconditions"`
}

// ResourceDeletionReceipt reports acceptance for the frozen target only.
// Callers must observe the target UID separately before reporting completion.
type ResourceDeletionReceipt struct {
	Accepted      bool                          `json:"accepted"`
	State         string                        `json:"state"`
	Kind          string                        `json:"kind"`
	Name          string                        `json:"name"`
	Namespace     string                        `json:"namespace,omitempty"`
	Preconditions ResourceMutationPreconditions `json:"preconditions"`
}

// ConditionalResourceMutationRequest is the Agent transport. Core derives the
// target from its authorized route rather than accepting an arbitrary API path.
type ConditionalResourceMutationRequest struct {
	Namespace     string                        `json:"namespace,omitempty"`
	Kind          string                        `json:"kind"`
	Name          string                        `json:"name"`
	Preconditions ResourceMutationPreconditions `json:"preconditions"`
}

type ConditionalResourceYAMLRequest struct {
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	ConditionalResourceYAMLInput
}
