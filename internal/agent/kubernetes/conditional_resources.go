package kubernetes

import (
	"context"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) GetResourceIdentity(ctx context.Context, namespace, kind, name string) (contractresource.ResourceIdentity, error) {
	gvr, namespaced, _, err := resourceGVRForKind(kind)
	if err != nil {
		return contractresource.ResourceIdentity{}, apierrors.NewBadRequest("unsupported resource kind")
	}
	resource, _, err := c.dynamicResource(gvr, namespaced, namespace, nil)
	if err != nil {
		return contractresource.ResourceIdentity{}, apierrors.NewBadRequest("invalid resource namespace")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return contractresource.ResourceIdentity{}, err
	}
	return contractruntime.ResourceIdentityFrom(current), nil
}

func (c *Client) ApplyResourceYAMLConditional(ctx context.Context, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error) {
	view, _, err := c.applyResourceYAML(ctx, namespace, kind, name, content, false, expected)
	return view, err
}

func (c *Client) DryRunResourceYAMLConditional(ctx context.Context, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error) {
	_, analysis, err := c.applyResourceYAML(ctx, namespace, kind, name, content, true, expected)
	return analysis, err
}

func (c *Client) DeleteResourceConditional(ctx context.Context, namespace, kind, name string, expected contractresource.ResourceMutationPreconditions) error {
	if err := expected.Validate(); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	gvr, namespaced, _, err := resourceGVRForKind(kind)
	if err != nil {
		return apierrors.NewBadRequest("unsupported resource kind")
	}
	resource, _, err := c.dynamicResource(gvr, namespaced, namespace, nil)
	if err != nil {
		return apierrors.NewBadRequest("invalid resource namespace")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return contractruntime.DeleteResourceConditional(queryCtx, resource, name, expected)
}
