package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

func (d *Direct) conditionalResource(ctx context.Context, clusterID, namespace, kind string) (dynamic.ResourceInterface, error) {
	gvr, namespaced, err := resourceGVRForKind(kind)
	if err != nil {
		return nil, err
	}
	if namespaced && strings.TrimSpace(namespace) == "" || !namespaced && namespace != "" {
		return nil, fmt.Errorf("%w: namespace does not match resource scope", apperrors.ErrInvalidArgument)
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return dynamicResource(bundle.Dynamic, gvr, namespaced, namespace), nil
}

func (d *Direct) GetResourceIdentity(ctx context.Context, clusterID, namespace, kind, name string) (contractresource.ResourceIdentity, error) {
	resource, err := d.conditionalResource(ctx, clusterID, namespace, kind)
	if err != nil {
		return contractresource.ResourceIdentity{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return contractresource.ResourceIdentity{}, conditionalResourceError(err)
	}
	return contractruntime.ResourceIdentityFrom(current), nil
}

func (d *Direct) ApplyResourceYAMLConditional(ctx context.Context, clusterID, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error) {
	view, _, err := d.applyResourceYAML(ctx, clusterID, namespace, kind, name, content, false, false, expected)
	return view, conditionalResourceError(err)
}

func (d *Direct) DryRunResourceYAMLConditional(ctx context.Context, clusterID, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error) {
	_, analysis, err := d.applyResourceYAML(ctx, clusterID, namespace, kind, name, content, true, false, expected)
	return analysis, conditionalResourceError(err)
}

func (d *Direct) DeleteResourceConditional(ctx context.Context, clusterID, namespace, kind, name string, expected contractresource.ResourceMutationPreconditions) error {
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	resource, err := d.conditionalResource(ctx, clusterID, namespace, kind)
	if err != nil {
		return conditionalResourceError(err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conditionalResourceError(contractruntime.DeleteResourceConditional(queryCtx, resource, name, expected))
}

func conditionalResourceError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case apierrors.IsBadRequest(err), apierrors.IsInvalid(err), errors.Is(err, apperrors.ErrInvalidArgument):
		return fmt.Errorf("%w: Kubernetes rejected conditional resource input", apperrors.ErrInvalidArgument)
	case apierrors.IsNotFound(err), errors.Is(err, apperrors.ErrNotFound):
		return fmt.Errorf("%w: Kubernetes resource not found", apperrors.ErrNotFound)
	case apierrors.IsForbidden(err), errors.Is(err, apperrors.ErrAccessDenied):
		return fmt.Errorf("%w: Kubernetes RBAC denied this operation", apperrors.ErrAccessDenied)
	case apierrors.IsConflict(err), errors.Is(err, apperrors.ErrConflict), errors.Is(err, contractruntime.ErrResourceOwnership):
		return fmt.Errorf("%w: Kubernetes resource identity, version, ownership or fields changed", apperrors.ErrConflict)
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		return apperrors.ErrUnsupportedOperation
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", apperrors.ErrClusterUnready, context.DeadlineExceeded)
	default:
		// Admission/webhook failures can echo arbitrary manifest values. Never
		// carry their raw message into application audit or approval summaries.
		return fmt.Errorf("%w: Kubernetes conditional resource request failed", apperrors.ErrClusterUnready)
	}
}
