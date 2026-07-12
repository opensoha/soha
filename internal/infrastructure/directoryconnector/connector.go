package directoryconnector

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrUnsupported = errors.New("directory connector capability is unsupported")

type Capability string

const (
	CapabilityOrganizations Capability = "organizations"
	CapabilityPeople        Capability = "people"
	CapabilityMemberships   Capability = "memberships"
	CapabilityEvents        Capability = "events"
)

type Capabilities struct {
	Organizations bool
	People        bool
	Memberships   bool
	Events        bool
}

type Organization struct {
	ExternalID       string
	Name             string
	ParentExternalID string
	SourceVersion    string
}

type Person struct {
	ExternalID      string
	ProviderSubject string
	UnionID         string
	Name            string
	Email           string
	Mobile          string
	AvatarURL       string
	Active          bool
	SourceVersion   string
}

type Membership struct {
	PersonExternalID       string
	OrganizationExternalID string
}

type Page[T any] struct {
	Items     []T
	NextToken string
}

// Connector reads and normalizes a provider directory. It never writes OpenSoha state.
type Connector interface {
	Provider() string
	Capabilities() Capabilities
	Validate(context.Context) error
	ListOrganizations(context.Context, string) (Page[Organization], error)
	ListPeople(context.Context, string, string) (Page[Person], error)
	ListMemberships(context.Context, string, string) (Page[Membership], error)
}

type ProviderError struct {
	Provider   string
	Operation  string
	StatusCode int
	Code       int
	RequestID  string
	RetryAfter time.Duration
	Message    string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s %s failed: status=%d code=%d request_id=%q message=%q", e.Provider, e.Operation, e.StatusCode, e.Code, e.RequestID, e.Message)
}

func (e *ProviderError) Temporary() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}

type Registry struct {
	factories map[string]func() Connector
	declared  map[string]Capabilities
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]func() Connector), declared: map[string]Capabilities{
		"wecom":    {},
		"dingtalk": {},
	}}
}

func (r *Registry) Register(provider string, factory func() Connector) error {
	if provider == "" || factory == nil {
		return errors.New("provider and factory are required")
	}
	if _, exists := r.factories[provider]; exists {
		return fmt.Errorf("directory connector %q already registered", provider)
	}
	r.factories[provider] = factory
	r.declared[provider] = factory().Capabilities()
	return nil
}

func (r *Registry) New(provider string) (Connector, error) {
	factory, ok := r.factories[provider]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q", ErrUnsupported, provider)
	}
	return factory(), nil
}

func (r *Registry) Capabilities(provider string) (Capabilities, bool) {
	c, ok := r.declared[provider]
	return c, ok
}
