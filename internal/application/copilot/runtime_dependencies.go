package copilot

import (
	"context"
	"fmt"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type WorkbenchToolInvoker interface {
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
}

type WorkbenchApprovalRequester interface {
	RequestToolApproval(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
}

type WorkbenchApprovalReader interface {
	ListApprovalRequests(context.Context, domainidentity.Principal, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error)
}

type WorkbenchModelCatalog interface {
	ListWorkbenchModels(context.Context, domainidentity.Principal, string) ([]appaigateway.WorkbenchModelOption, error)
	ResolveWorkbenchModelOption(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchModelOption, error)
}

type WorkbenchCapabilityCatalog interface {
	WorkbenchCapabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error)
}

type NativeGatewayAuthorizationReader interface {
	WorkbenchAuthorizationRevision(context.Context, domainidentity.Principal) (string, error)
}

type NativeTaskPlanValidator interface {
	ValidateCapabilityPlan(context.Context, domainidentity.Principal, domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityPlanValidation, error)
}

type WorkbenchImageInvoker interface {
	InvokeWorkbenchImage(context.Context, domainidentity.Principal, appaigateway.WorkbenchImageRequest) ([]byte, error)
}

// RuntimeDependencies binds the Gateway capabilities after both services exist.
// Image is optional and an absent image generator reports unsupported.
type RuntimeDependencies struct {
	Model                WorkbenchModelInvoker
	Stream               WorkbenchModelStreamInvoker
	Tools                WorkbenchToolInvoker
	ApprovalRequests     WorkbenchApprovalRequester
	Approvals            WorkbenchApprovalReader
	ModelCatalog         WorkbenchModelCatalog
	CapabilityCatalog    WorkbenchCapabilityCatalog
	GatewayAuthorization NativeGatewayAuthorizationReader
	PlanValidator        NativeTaskPlanValidator
	Image                WorkbenchImageInvoker
}

// ConfigureRuntime completes assembly before the service is exposed or started.
func (s *Service) ConfigureRuntime(deps RuntimeDependencies) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.runtimeConfigured {
		return fmt.Errorf("%w: Copilot runtime is already configured", apperrors.ErrConflict)
	}
	if err := s.validateRuntimeDependencies(deps); err != nil {
		return err
	}
	s.runtime = deps
	s.runtimeConfigured = true
	return nil
}

func (s *Service) ValidateRuntimeDependencies() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.validateRuntimeDependencies(s.runtime)
}

func (s *Service) validateRuntimeDependencies(deps RuntimeDependencies) error {
	for _, dependency := range []struct {
		name  string
		value any
	}{
		{"AI settings", s.settings},
		{"model", deps.Model},
		{"model stream", deps.Stream},
		{"tools", deps.Tools},
		{"capability catalog", deps.CapabilityCatalog},
		{"approval requests", deps.ApprovalRequests},
		{"approval reader", deps.Approvals},
		{"model catalog", deps.ModelCatalog},
	} {
		if isNilDependency(dependency.value) {
			return fmt.Errorf("copilot runtime: %s dependency is required", dependency.name)
		}
	}
	if deps.Image != nil && isNilDependency(deps.Image) {
		return fmt.Errorf("copilot runtime: image dependency is typed nil")
	}
	// Native is optional, but any Native-specific port opts into the complete group.
	if deps.GatewayAuthorization != nil || deps.PlanValidator != nil || s.nativeAuthorization != nil || s.nativeRuns != nil || s.nativeTaskParents != nil || s.nativeTasks != nil {
		return s.validateNativeRuntime(deps)
	}
	return nil
}

func (s *Service) validateNativeRuntime(deps RuntimeDependencies) error {
	for _, dependency := range []struct {
		name  string
		value any
	}{
		{"model stream", deps.Stream},
		{"tools", deps.Tools},
		{"capability catalog", deps.CapabilityCatalog},
		{"Native Gateway authorization", deps.GatewayAuthorization},
		{"Native access authorization", s.nativeAuthorization},
		{"Native identity resolver", s.agentPrincipals},
		{"Native run store", s.nativeRuns},
		{"Native task parent store", s.nativeTaskParents},
		{"Native plan validator", deps.PlanValidator},
		{"Native task executor", s.nativeTasks},
	} {
		if isNilDependency(dependency.value) {
			return fmt.Errorf("%w: copilot runtime: %s dependency is required", apperrors.ErrUnsupportedOperation, dependency.name)
		}
	}
	return nil
}
