package providerportal

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appidentityprovider "github.com/opensoha/soha/internal/application/identityprovider"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type OnboardingInput struct {
	Application domainportal.ApplicationInput   `json:"application"`
	AccessMode  string                          `json:"accessMode"`
	Provider    *domainprovider.ProviderInput   `json:"provider,omitempty"`
	OIDCClient  *domainprovider.OIDCClientInput `json:"oidcClient,omitempty"`
}

type OnboardingResult struct {
	Application domainportal.Application          `json:"application"`
	Provider    *domainprovider.Provider          `json:"provider,omitempty"`
	OIDCClient  *domainprovider.OIDCClientCreated `json:"oidcClient,omitempty"`
}

type OnboardingRepository interface {
	WithinOnboardingTransaction(context.Context, func(domainportal.Repository, domainprovider.Repository) error) error
}

type OnboardingService struct {
	repo         OnboardingRepository
	applications *Service
	providers    *appidentityprovider.Service
}

func NewOnboarding(repo OnboardingRepository, applications *Service, providers *appidentityprovider.Service) *OnboardingService {
	return &OnboardingService{repo: repo, applications: applications, providers: providers}
}

func (s *OnboardingService) OnboardApplication(ctx context.Context, principal domainidentity.Principal, input OnboardingInput) (OnboardingResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.applications.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermIdentityApplicationsManage, "create")); err != nil {
		return OnboardingResult{}, err
	}
	if input.Provider != nil {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.applications.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermIdentityProvidersManage, "create")); err != nil {
			return OnboardingResult{}, err
		}
	}
	if err := validateOnboardingInput(input); err != nil {
		return OnboardingResult{}, err
	}
	input.Application.Status = domainportal.ApplicationStatusDisabled
	var result OnboardingResult
	audit := &onboardingAudit{}
	err := s.repo.WithinOnboardingTransaction(ctx, func(applications domainportal.Repository, providers domainprovider.Repository) error {
		portal := *s.applications
		portal.repo, portal.audit = applications, audit
		var err error
		result.Application, err = portal.CreateApplication(ctx, principal, input.Application)
		if err != nil || input.Provider == nil {
			return err
		}
		providerService := s.providers.ForTransaction(providers, audit)
		return createOnboardingProvider(ctx, principal, input, &result, applications, providerService)
	})
	if err != nil {
		return OnboardingResult{}, err
	}
	if s.applications.audit != nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, entry := range audit.entries {
			_ = s.applications.audit.Record(auditCtx, entry)
		}
	}
	return result, nil
}

func createOnboardingProvider(ctx context.Context, principal domainidentity.Principal, input OnboardingInput, result *OnboardingResult, applications domainportal.Repository, providers *appidentityprovider.Service) error {
	providerInput := *input.Provider
	providerInput.ApplicationID = result.Application.ID
	provider, err := providers.CreateProvider(ctx, principal, providerInput)
	if err != nil {
		return err
	}
	if err := applications.ValidateProviderBinding(ctx, provider.ID, result.Application.ID, result.Application.ProviderType); err != nil {
		return err
	}
	result.Provider = &provider
	result.Application.ProviderID = provider.ID
	result.Application, err = applications.UpdateApplication(ctx, result.Application)
	if err != nil || input.OIDCClient == nil {
		return err
	}
	client, err := providers.CreateOIDCClient(ctx, principal, provider.ID, *input.OIDCClient)
	if err != nil {
		return err
	}
	result.OIDCClient = &client
	return nil
}

func validateOnboardingInput(input OnboardingInput) error {
	if strings.TrimSpace(input.Application.ProviderID) != "" {
		return fmt.Errorf("%w: onboarding cannot attach existing resources", apperrors.ErrInvalidArgument)
	}
	switch input.AccessMode {
	case "all_authenticated":
		if len(input.Application.Assignments) != 0 {
			return fmt.Errorf("%w: all-authenticated access cannot include assignments", apperrors.ErrInvalidArgument)
		}
	case "restricted":
		allow := false
		for _, assignment := range input.Application.Assignments {
			if assignment.Effect == domainportal.AssignmentEffectAllow {
				allow = true
			}
		}
		if !allow {
			return fmt.Errorf("%w: restricted access requires an explicit allow assignment", apperrors.ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: accessMode must be explicitly selected", apperrors.ErrInvalidArgument)
	}
	if input.Provider != nil && (strings.TrimSpace(input.Provider.ApplicationID) != "" || input.Provider.Type != input.Application.ProviderType || input.Application.ProviderType == domainportal.ProviderTypeLink) {
		return fmt.Errorf("%w: new provider must match the application type and have no existing application binding", apperrors.ErrInvalidArgument)
	}
	if input.OIDCClient != nil && (input.Provider == nil || input.Provider.Type != domainprovider.ProviderTypeOIDC || strings.TrimSpace(input.OIDCClient.ProviderID) != "") {
		return fmt.Errorf("%w: first OIDC client requires a new OIDC provider", apperrors.ErrInvalidArgument)
	}
	return nil
}

type onboardingAudit struct{ entries []domainaudit.Entry }

func (a *onboardingAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.entries = append(a.entries, entry)
	return nil
}
