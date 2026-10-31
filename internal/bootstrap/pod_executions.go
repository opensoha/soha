package bootstrap

import (
	"context"
	"fmt"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	appresource "github.com/opensoha/soha/internal/application/resource"
	"go.uber.org/zap"
)

func bindKubernetesGatewayCapabilities(resources *appresource.Service, identity *appidentity.Service, gateway *appaigateway.Service, receipts appresource.ConditionalMutationRepository) error {
	if resources == nil || identity == nil || gateway == nil {
		return fmt.Errorf("kubernetes Gateway resource, identity and authorizer are required")
	}
	mutations, err := appresource.NewGatewayConditionalMutations(resources.GenericResources(), receipts, identity.CurrentGatewayMutationPrincipal, gateway.CheckExecutionAuthorization)
	if err != nil {
		return fmt.Errorf("bind conditional Kubernetes mutation receipts: %w", err)
	}
	conditional, err := appaigateway.NewConditionalResourceCapabilityProvider(mutations)
	if err != nil {
		return fmt.Errorf("build conditional Kubernetes resource capabilities: %w", err)
	}
	gateway.AddCapabilityProviders(conditional)
	return bindGatewayPodExecutions(resources.Workloads(), identity, gateway)
}

func bindGatewayPodExecutions(workloads *appresource.Workloads, identity *appidentity.Service, gateway *appaigateway.Service) error {
	if identity == nil || gateway == nil {
		return fmt.Errorf("gateway Pod execution identity and authorizer are required")
	}
	service, err := appresource.NewGatewayPodExecutions(workloads, identity.CurrentDelegatedExecutionPrincipal, gateway.CheckExecutionAuthorization)
	if err != nil {
		return fmt.Errorf("bind Gateway Pod execution: %w", err)
	}
	provider, err := appaigateway.NewPodExecutionCapabilityProvider(service)
	if err != nil {
		return fmt.Errorf("build Gateway Pod execution capabilities: %w", err)
	}
	gateway.AddCapabilityProviders(provider)
	return nil
}

type podExecutionCleaner interface{ Cleanup(context.Context) error }

func startPlatformReceiptCleanup(ctx context.Context, repository podExecutionCleaner, logger *zap.Logger, event string) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run, cancel := context.WithTimeout(ctx, 4*time.Second)
				err := repository.Cleanup(run)
				cancel()
				if err != nil && ctx.Err() == nil {
					logger.Warn("Platform execution receipt cleanup failed", zap.String("event", event))
				}
			}
		}
	}()
}
