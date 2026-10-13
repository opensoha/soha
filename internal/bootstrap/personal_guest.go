package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	guestapi "github.com/opensoha/soha/internal/api/personalguest"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/infrastructure/personalbrowser"
	guestissuer "github.com/opensoha/soha/internal/infrastructure/personalguest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalGuestAuthorizer struct {
	identity interface {
		CurrentPrincipal(context.Context, string) (identity.Principal, error)
	}
	access interface {
		GetPersonalAgentAccess(context.Context, identity.Principal) (domain.PersonalAgentAccess, error)
	}
	vm interface {
		ValidatePersonalGuestVM(context.Context, identity.Principal, guest.Binding) error
	}
}

func (a personalGuestAuthorizer) Authorize(ctx context.Context, binding guest.Binding) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	principal, err := a.identity.CurrentPrincipal(ctx, binding.OwnerID)
	if err != nil {
		return err
	}
	if principal.UserID != binding.OwnerID {
		return apperrors.ErrAccessDenied
	}
	access, err := a.access.GetPersonalAgentAccess(ctx, principal)
	if err != nil {
		return err
	}
	if access.UserID != binding.OwnerID || access.Status != "approved" {
		return apperrors.ErrAccessDenied
	}
	// shortcut: observe the provider per request; add a bounded freshness cache only after measured load requires it.
	return a.vm.ValidatePersonalGuestVM(ctx, principal, binding)
}

func (a personalGuestAuthorizer) AuthorizeData(ctx context.Context, binding guest.Binding) error {
	if err := a.Authorize(ctx, binding); err != nil {
		return err
	}
	principal, err := a.identity.CurrentPrincipal(ctx, binding.OwnerID)
	if err != nil {
		return err
	}
	vm, ok := a.vm.(interface {
		AuthorizePersonalGuestMutation(context.Context, identity.Principal, guest.Binding) error
	})
	if !ok {
		return apperrors.ErrAccessDenied
	}
	return vm.AuthorizePersonalGuestMutation(ctx, principal, binding)
}

func newPersonalGuestServer(cfg cfgpkg.PersonalGuestConfig, core *coreServices, delivery *deliveryServices, repos *repositories) (*http.Server, *appcopilot.PersonalGuestPreparationService, error) {
	if !cfg.Enabled {
		return nil, nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if core == nil || core.identityService == nil || core.auditService == nil || delivery == nil || delivery.copilotService == nil || delivery.virtualizationService == nil || repos == nil || repos.copilotRepository == nil {
		return nil, nil, fmt.Errorf("personal guest requires current identity, access, VM, audit and storage services")
	}
	tlsConfig, err := networkidentity.LoadServerTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.ClientCAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load personal guest TLS: %w", err)
	}
	tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	authorizer := personalGuestAuthorizer{identity: core.identityService, access: delivery.copilotService, vm: delivery.virtualizationService}
	service, err := appcopilot.NewPersonalGuestService(repos.copilotRepository, authorizer.Authorize, core.auditService)
	if err != nil {
		return nil, nil, err
	}
	var preparation *appcopilot.PersonalGuestPreparationService
	if cfg.IssuerKeyFile != "" {
		issuer, err := guestissuer.LoadIssuer(cfg.TLS.ClientCAFile, cfg.IssuerKeyFile)
		if err != nil {
			return nil, nil, err
		}
		service.ConfigureRenewal(issuer)
		issuer.CertificateTTL = time.Duration(cfg.CertificateTTLSeconds) * time.Second
		preparation, err = appcopilot.NewPersonalGuestPreparationService(repos.copilotRepository, service, issuer, cfg.Endpoint)
		if err != nil {
			return nil, nil, err
		}
		if cfg.ArtifactFile != "" {
			artifact, err := guestissuer.LoadArtifact(cfg.ArtifactFile)
			if err != nil {
				return nil, nil, err
			}
			vm := delivery.virtualizationService
			vm.ConfigurePersonalGuest(authorizer.AuthorizeData, repos.copilotRepository.GuestDataCheckpoint, repos.copilotRepository.WithinPersonalGuestTransaction)
			digest := service.ConfigureProvisioning(artifact, vm.PersonalGuestDataGrant)
			preparation.ConfigureProvisioning(vm, digest)
			vm.ConfigurePersonalGuestTools(repos.copilotRepository.GuestToolsCheckpoint)
			service.ConfigureTools(vm.PersonalGuestToolsGrant)
			vm.ConfigurePersonalRuntime(digest, repos.copilotRepository.AuthorizePersonalGuestRuntime)
			vm.ConfigurePersonalRuntimeCompletion(repos.copilotRepository.CompletePersonalRuntime)
			service.ConfigureRuntime(vm.PersonalGuestRuntimeGrant)
			service.ConfigureBrowserNetwork(personalbrowser.NewFetcher())
			service.ConfigureDataLease(func(ctx context.Context, binding guest.Binding) (guest.DataPlan, error) {
				if err := authorizer.AuthorizeData(ctx, binding); err != nil {
					return guest.DataPlan{}, err
				}
				return repos.copilotRepository.PersonalGuestDataLease(ctx, binding)
			})
		}
	}
	router, err := guestapi.NewRouter(service)
	if err != nil {
		return nil, nil, err
	}
	return &http.Server{Addr: cfg.Addr, Handler: router, TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8192}, preparation, nil
}
