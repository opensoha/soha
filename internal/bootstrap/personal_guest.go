package bootstrap

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	guestapi "github.com/opensoha/soha/internal/api/personalguest"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	"github.com/opensoha/soha/internal/application/personalenvironment"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/infrastructure/personalbrowser"
	guestissuer "github.com/opensoha/soha/internal/infrastructure/personalguest"
	"github.com/opensoha/soha/internal/networkidentity"
)

func newPersonalGuestServer(cfg cfgpkg.PersonalGuestConfig, core *coreServices, delivery *deliveryServices, repos *repositories) (*http.Server, *appcopilot.PersonalGuestPreparationService, error) {
	if !cfg.Enabled {
		return nil, nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if core == nil || core.auditService == nil || delivery == nil || repos == nil {
		return nil, nil, fmt.Errorf("personal guest requires current identity, access, VM, audit and storage services")
	}
	authorizer, err := personalenvironment.NewGuestAccess(personalenvironment.GuestAccessDependencies{
		Identity: core.identityService, Approvals: delivery.copilotService,
		VM: delivery.virtualizationService, Leases: repos.copilotRepository,
	})
	if err != nil {
		return nil, nil, err
	}
	tlsConfig, err := networkidentity.LoadServerTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.ClientCAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load personal guest TLS: %w", err)
	}
	tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
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
			service.ConfigureDataLease(authorizer.DataLease)
		}
	}
	router, err := guestapi.NewRouter(service)
	if err != nil {
		return nil, nil, err
	}
	return &http.Server{Addr: cfg.Addr, Handler: router, TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8192}, preparation, nil
}
