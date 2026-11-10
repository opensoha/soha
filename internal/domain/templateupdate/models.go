// Package templateupdate owns frozen configuration adoption candidates. Scheduling,
// approval and execution remain owned by DeliveryBatch and DeliveryPlan.
package templateupdate

import (
	"context"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

type Filter struct {
	Kind                                                           sohaapi.TemplateUpdateKind
	TemplateID, ApplicationID, ServiceID, ApplicationEnvironmentID string
	Page, PageSize                                                 int
}

// Binding is private source evidence, never an HTTP response or a Plan impact.
type Binding struct {
	Target                sohaapi.TemplateUpdateTarget
	Application           domainapp.App
	Service               domainapp.Service
	Environment           domaincatalog.ApplicationEnvironment
	Environments          []domaincatalog.ApplicationEnvironment
	ReleaseTarget         domaincatalog.ReleaseTarget
	Package               domainmanifest.Package
	ManifestBinding       domainmanifest.EnvironmentBinding
	BuildSourceID         string
	Parameters            map[string]any
	EnvironmentParameters map[string]any
}

type FrozenCandidate struct {
	PreviewID      string
	Candidate      sohaapi.TemplateUpdateCandidate
	Binding        Binding
	TemplateDigest string
	Selection      sohaapi.TemplateUpdateSelection
	ExpiresAt      time.Time
	AppliedPlanID  string
	PolicyID       string
	PolicyVersion  int64
	Sensitive      bool
}

type Policy struct {
	ApplicationID      string
	Value              sohaapi.TemplateUpdatePolicy
	SubjectID, TokenID string
	AttemptVersion     int64
	AttemptPreviewID   string
}

type Repository interface {
	SavePreview(context.Context, sohaapi.TemplateUpdatePreview, []FrozenCandidate) error
	GetPreview(context.Context, string) (sohaapi.TemplateUpdatePreview, error)
	GetCandidate(context.Context, string) (FrozenCandidate, error)
	ApplyCandidate(context.Context, string, string, func(context.Context, FrozenCandidate) error) error
	ListPolicies(context.Context, Filter) ([]Policy, error)
	SavePolicy(context.Context, Policy, int64) (Policy, error)
	ListDuePolicies(context.Context, int) ([]Policy, error)
	WithPolicy(context.Context, string, int64, func(context.Context, *Policy) error) error
}
