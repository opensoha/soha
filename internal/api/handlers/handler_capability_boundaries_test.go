package handlers

import (
	"reflect"
	"testing"
)

func TestHandlerCapabilityInterfacesStayFocused(t *testing.T) {
	t.Parallel()

	interfaces := map[string]reflect.Type{
		"AnnouncementReader":              reflect.TypeOf((*AnnouncementReader)(nil)).Elem(),
		"AnnouncementWriter":              reflect.TypeOf((*AnnouncementWriter)(nil)).Elem(),
		"ApplicationCatalogService":       reflect.TypeOf((*ApplicationCatalogService)(nil)).Elem(),
		"ApplicationComponentService":     reflect.TypeOf((*ApplicationComponentService)(nil)).Elem(),
		"ApplicationGitService":           reflect.TypeOf((*ApplicationGitService)(nil)).Elem(),
		"ApplicationEnvironmentService":   reflect.TypeOf((*ApplicationEnvironmentService)(nil)).Elem(),
		"BuildTemplateService":            reflect.TypeOf((*BuildTemplateService)(nil)).Elem(),
		"WorkflowTemplateService":         reflect.TypeOf((*WorkflowTemplateService)(nil)).Elem(),
		"PluginMarketplaceService":        reflect.TypeOf((*PluginMarketplaceService)(nil)).Elem(),
		"PluginInventoryService":          reflect.TypeOf((*PluginInventoryService)(nil)).Elem(),
		"PluginLifecycleService":          reflect.TypeOf((*PluginLifecycleService)(nil)).Elem(),
		"PluginExtensionService":          reflect.TypeOf((*PluginExtensionService)(nil)).Elem(),
		"IdentitySettingsService":         reflect.TypeOf((*IdentitySettingsService)(nil)).Elem(),
		"MonitoringSettingsService":       reflect.TypeOf((*MonitoringSettingsService)(nil)).Elem(),
		"AISettingsService":               reflect.TypeOf((*AISettingsService)(nil)).Elem(),
		"BrandingSettingsService":         reflect.TypeOf((*BrandingSettingsService)(nil)).Elem(),
		"IdentityAuthService":             reflect.TypeOf((*IdentityAuthService)(nil)).Elem(),
		"IdentityProfileService":          reflect.TypeOf((*IdentityProfileService)(nil)).Elem(),
		"IdentityFederationService":       reflect.TypeOf((*IdentityFederationService)(nil)).Elem(),
		"IdentitySessionService":          reflect.TypeOf((*IdentitySessionService)(nil)).Elem(),
		"IdentityStreamTicketService":     reflect.TypeOf((*IdentityStreamTicketService)(nil)).Elem(),
		"DockerHostService":               reflect.TypeOf((*DockerHostService)(nil)).Elem(),
		"DockerProjectService":            reflect.TypeOf((*DockerProjectService)(nil)).Elem(),
		"DockerProjectRuntimeService":     reflect.TypeOf((*DockerProjectRuntimeService)(nil)).Elem(),
		"DockerProjectStorageService":     reflect.TypeOf((*DockerProjectStorageService)(nil)).Elem(),
		"DockerServiceRuntimeService":     reflect.TypeOf((*DockerServiceRuntimeService)(nil)).Elem(),
		"DockerPortMappingService":        reflect.TypeOf((*DockerPortMappingService)(nil)).Elem(),
		"DockerTemplateService":           reflect.TypeOf((*DockerTemplateService)(nil)).Elem(),
		"DockerOperationService":          reflect.TypeOf((*DockerOperationService)(nil)).Elem(),
		"DockerRunnerOperationService":    reflect.TypeOf((*DockerRunnerOperationService)(nil)).Elem(),
		"DeliveryApplicationService":      reflect.TypeOf((*DeliveryApplicationService)(nil)).Elem(),
		"DeliveryReleaseService":          reflect.TypeOf((*DeliveryReleaseService)(nil)).Elem(),
		"DeliveryExecutionQueryService":   reflect.TypeOf((*DeliveryExecutionQueryService)(nil)).Elem(),
		"DeliveryRuntimeService":          reflect.TypeOf((*DeliveryRuntimeService)(nil)).Elem(),
		"DeliveryBlueprintService":        reflect.TypeOf((*DeliveryBlueprintService)(nil)).Elem(),
		"DeliveryDraftPlanService":        reflect.TypeOf((*DeliveryDraftPlanService)(nil)).Elem(),
		"DeliveryExecutionActionService":  reflect.TypeOf((*DeliveryExecutionActionService)(nil)).Elem(),
		"DeliveryRunnerService":           reflect.TypeOf((*DeliveryRunnerService)(nil)).Elem(),
		"VirtualizationConnectionService": reflect.TypeOf((*VirtualizationConnectionService)(nil)).Elem(),
		"VirtualizationSyncService":       reflect.TypeOf((*VirtualizationSyncService)(nil)).Elem(),
		"VirtualizationVMService":         reflect.TypeOf((*VirtualizationVMService)(nil)).Elem(),
		"VirtualizationImageService":      reflect.TypeOf((*VirtualizationImageService)(nil)).Elem(),
		"VirtualizationFlavorService":     reflect.TypeOf((*VirtualizationFlavorService)(nil)).Elem(),
		"VirtualizationOperationService":  reflect.TypeOf((*VirtualizationOperationService)(nil)).Elem(),
		"VirtualizationRuntimeService":    reflect.TypeOf((*VirtualizationRuntimeService)(nil)).Elem(),
	}

	for name, interfaceType := range interfaces {
		interfaceType := interfaceType
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := interfaceType.NumMethod(); got > 7 {
				t.Fatalf("%s has %d methods; focused handler capabilities must have at most 7", name, got)
			}
		})
	}
}

func TestProductionHandlersDoNotStoreBroadInterfaces(t *testing.T) {
	t.Parallel()

	handlers := []any{
		AnnouncementHandler{},
		ApplicationHandler{},
		CatalogHandler{},
		PluginHandler{},
		SettingsHandler{},
		AuthHandler{},
		DockerHandler{},
		DeliveryHandler{},
		VirtualizationHandler{},
	}

	for _, handler := range handlers {
		handlerType := reflect.TypeOf(handler)
		t.Run(handlerType.Name(), func(t *testing.T) {
			t.Parallel()
			for i := 0; i < handlerType.NumField(); i++ {
				field := handlerType.Field(i)
				if field.Type.Kind() == reflect.Interface && field.Type.NumMethod() > 7 {
					t.Errorf("field %s stores a broad interface with %d methods", field.Name, field.Type.NumMethod())
				}
			}
		})
	}
}
