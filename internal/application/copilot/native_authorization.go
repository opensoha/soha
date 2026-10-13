package copilot

import (
	"context"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Access owns namespace selectors, resource grants and application target mappings.
type NativeAuthorizationReader interface {
	ExecutionAuthorizationRevision(context.Context, domainidentity.Principal) (string, error)
}

type nativeGatewayAuthorizationReader interface {
	WorkbenchAuthorizationRevision(context.Context, domainidentity.Principal) (string, error)
}

func (s *Service) freezeNativeGatewayAuthorization(ctx context.Context, principal domainidentity.Principal) (string, error) {
	reader, ok := s.workbenchInvoker.(nativeGatewayAuthorizationReader)
	if !ok || isNilDependency(reader) {
		return "", apperrors.ErrUnsupportedOperation
	}
	revision, err := reader.WorkbenchAuthorizationRevision(ctx, principal)
	if err != nil {
		return "", err
	}
	if len(revision) != 64 {
		return "", apperrors.ErrInvalidArgument
	}
	return revision, nil
}

func (s *Service) freezeNativeAuthorization(ctx context.Context, principal domainidentity.Principal) (string, error) {
	if isNilDependency(s.nativeAuthorization) {
		return "", fmt.Errorf("%w: Native authorization ceiling is unavailable", apperrors.ErrUnsupportedOperation)
	}
	revision, err := s.nativeAuthorization.ExecutionAuthorizationRevision(ctx, principal)
	if err != nil {
		return "", err
	}
	if len(revision) != 64 {
		return "", apperrors.ErrInvalidArgument
	}
	return revision, nil
}

func (s *Service) freezeNativeAuthorizationCeilings(ctx context.Context, principal domainidentity.Principal) (string, string, error) {
	access, err := s.freezeNativeAuthorization(ctx, principal)
	if err != nil {
		return "", "", err
	}
	gateway, err := s.freezeNativeGatewayAuthorization(ctx, principal)
	return access, gateway, err
}

func (s *Service) nativeAuthorizationContext(ctx context.Context, principal domainidentity.Principal, run domaincopilot.AgentRun, required bool) (context.Context, error) {
	expected := stringValue(run.Input["_sohaAuthorizationCeiling"])
	if expected == "" && !required {
		_, hasAccess := run.Input["_sohaAuthorizationCeiling"]
		_, hasGateway := run.Input["_sohaGatewayAuthorizationCeiling"]
		if !hasAccess && !hasGateway {
			return ctx, nil // Historical read-only Native runs retain compatibility.
		}
	}
	current, err := s.freezeNativeAuthorization(ctx, principal)
	if err != nil {
		return ctx, err
	}
	if expected == "" || expected != current {
		return ctx, fmt.Errorf("%w: Native authorization scope changed", apperrors.ErrAccessDenied)
	}
	gatewayExpected := stringValue(run.Input["_sohaGatewayAuthorizationCeiling"])
	gatewayCurrent, err := s.freezeNativeGatewayAuthorization(ctx, principal)
	if err != nil {
		return ctx, err
	}
	if gatewayExpected == "" || gatewayExpected != gatewayCurrent {
		return ctx, fmt.Errorf("%w: Native Gateway authorization scope changed", apperrors.ErrAccessDenied)
	}
	ctx = appaccess.WithExecutionAuthorizationCeiling(ctx, expected)
	return appaigateway.WithWorkbenchAuthorizationCeiling(ctx, gatewayExpected), nil
}
