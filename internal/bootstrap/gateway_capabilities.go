package bootstrap

import (
	"fmt"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
)

// Each capability has one owner; registration errors abort assembly before workers start.
func bindGatewayCapabilities(aiGatewayService *appaigateway.Service, core *coreServices, delivery *deliveryServices, repos *repositories) error {
	draftCapabilities, err := appaigateway.NewDraftCapabilityProvider(delivery.deliveryService)
	if err != nil {
		return fmt.Errorf("build draft capabilities: %w", err)
	}
	virtualizationCapabilities, err := appaigateway.NewVirtualizationCapabilityProvider(delivery.virtualizationService)
	if err != nil {
		return fmt.Errorf("build virtualization capabilities: %w", err)
	}
	resourceCreationCapabilities, err := appaigateway.NewResourceCreationCapabilityProvider(core.resourceService.ResourceCreation())
	if err != nil {
		return fmt.Errorf("build Kubernetes creation capabilities: %w", err)
	}
	if err := aiGatewayService.SetCapabilityProviders(draftCapabilities, virtualizationCapabilities, resourceCreationCapabilities, appaigateway.BuiltinCapabilityProvider{}); err != nil {
		return fmt.Errorf("register core Gateway capabilities: %w", err)
	}
	if err := bindKubernetesGatewayCapabilities(core.resourceService, core.identityService, aiGatewayService, repos.conditionalMutationRepository); err != nil {
		return err
	}
	deliveryCapabilities, err := appaigateway.NewDeliveryCapabilityProvider(delivery.workflowService)
	if err != nil {
		return fmt.Errorf("build delivery capabilities: %w", err)
	}
	observabilityCapabilities, err := appaigateway.NewObservabilityCapabilityProvider(core.monitoringService.AssessMetrics)
	if err != nil {
		return fmt.Errorf("build observability capabilities: %w", err)
	}
	professionalCapabilities, err := appaigateway.NewProfessionalCapabilityProvider(delivery.copilotService.RunProfessionalCapability)
	if err != nil {
		return fmt.Errorf("build professional capabilities: %w", err)
	}
	dockerProjectCapabilities, err := appaigateway.NewDockerProjectCapabilityProvider(delivery.dockerService)
	if err != nil {
		return fmt.Errorf("build Docker project capabilities: %w", err)
	}
	if err := aiGatewayService.AddCapabilityProviders(deliveryCapabilities, observabilityCapabilities, professionalCapabilities, dockerProjectCapabilities,
		appaigateway.NewKnowledgeCapabilityProvider(delivery.knowledgeService), appaigateway.NewPersonalEnvironmentCapabilityProvider(delivery.copilotService)); err != nil {
		return fmt.Errorf("register application Gateway capabilities: %w", err)
	}
	return nil
}
