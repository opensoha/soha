package aiproduction

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPersonalDockerPolicyRequiresOwnerQuotaAndScopedAgentAssignment(t *testing.T) {
	for _, test := range []struct {
		name         string
		change       func(*EnvironmentTemplate)
		validatorErr error
		want         error
		checked      bool
	}{
		{"configured", func(*EnvironmentTemplate) {}, nil, nil, true},
		{"denied node", func(*EnvironmentTemplate) {}, apperrors.ErrAccessDenied, apperrors.ErrAccessDenied, true},
		{"non-container", func(p *EnvironmentTemplate) { p.Backend = "kubernetes" }, nil, apperrors.ErrInvalidArgument, false},
		{"unknown id", func(p *EnvironmentTemplate) { p.PersonalDocker.AgentClusterID = uuid.Nil }, nil, apperrors.ErrInvalidArgument, false},
		{"unbounded time", func(p *EnvironmentTemplate) { p.PersonalDocker.LeaseSeconds = 3601 }, nil, apperrors.ErrInvalidArgument, false},
		{"network", func(p *EnvironmentTemplate) { p.PersonalDocker.Quota.NetworkEgress = true }, nil, apperrors.ErrInvalidArgument, false},
		{"quota", func(p *EnvironmentTemplate) { p.PersonalDocker.Quota.DiskBytes = 1 }, nil, apperrors.ErrInvalidArgument, false},
		{"conflicting policy", func(p *EnvironmentTemplate) { p.NetworkPolicy = map[string]any{"egress": true} }, nil, apperrors.ErrInvalidArgument, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := EnvironmentTemplate{ID: "personal", Name: "Personal Docker", Backend: "container", IsolationMode: "disposable-write", PersonalDocker: &sohaapi.PersonalDockerEnvironmentPolicy{
				AgentClusterID: uuid.MustParse("00000000-0000-4000-8000-000000000048"), LeaseSeconds: 600,
				Quota: sohaapi.PersonalEnvironmentQuota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
			test.change(&policy)
			store := newMemoryStore()
			checked := false
			service, err := New(store, func(_ context.Context, actor domainidentity.Principal, id string) error {
				checked = true
				if actor.UserID != "operator" || id != policy.PersonalDocker.AgentClusterID.String() {
					t.Fatal("assignment lost authenticated actor or node")
				}
				return test.validatorErr
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.PutEnvironmentTemplateForPrincipal(t.Context(), domainidentity.Principal{UserID: "operator"}, policy)
			if !errors.Is(err, test.want) || checked != test.checked || (len(store.templates) == 1) != (test.want == nil) {
				t.Fatalf("error=%v checked=%t saved=%d", err, checked, len(store.templates))
			}
			if _, err := service.PutEnvironmentTemplate(t.Context(), policy); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatal("unscoped writer bypassed personal policy assignment")
			}
		})
	}
}

func TestPersonalEnvironmentPolicyListUsesSameActiveAndSupportedRulesAsWake(t *testing.T) {
	base := EnvironmentTemplate{ID: "personal", Name: "Personal Docker", Status: "active", Backend: "container", IsolationMode: "read-only", PersonalDocker: &sohaapi.PersonalDockerEnvironmentPolicy{
		AgentClusterID: uuid.MustParse("00000000-0000-4000-8000-000000000048"), LeaseSeconds: 600,
		Quota: sohaapi.PersonalEnvironmentQuota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
	store := newMemoryStore()
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*EnvironmentTemplate){func(p *EnvironmentTemplate) { p.Status = "disabled" }, func(p *EnvironmentTemplate) { p.PersonalDocker = nil }, func(p *EnvironmentTemplate) { p.IsolationMode = "unrestricted" }} {
		item := base
		change(&item)
		store.templates = append(store.templates, item)
	}
	store.templates = append(store.templates, base)
	policies, err := service.ListPersonalEnvironmentPolicies(t.Context())
	if err != nil || len(policies) != 1 || policies[0].Name != base.Name || policies[0].Mode != "observe-only" {
		t.Fatalf("inactive or unsupported choice: %+v %v", policies, err)
	}
}
