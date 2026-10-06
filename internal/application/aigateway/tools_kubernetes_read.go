package aigateway

import (
	"context"
	"fmt"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) invokeKubernetesDiagnosticReadTool(ctx context.Context, principal domainidentity.Principal, name string, req kubernetesToolRequest, related map[string]any) (any, map[string]any, error) {
	resources, ok := s.resources.(KubernetesDiagnosticReadService)
	if !ok {
		return nil, related, fmt.Errorf("%w: Kubernetes diagnostic read service is unavailable", apperrors.ErrUnsupportedOperation)
	}
	switch name {
	case "k8s.crds.list":
		items, err := resources.ListCRDs(ctx, principal, req.ClusterID)
		related["count"] = len(items)
		return items, related, err
	case "k8s.custom_resources.list":
		req.CRDName = strings.TrimSpace(req.CRDName)
		if req.CRDName == "" {
			return nil, related, fmt.Errorf("%w: crdName is required", apperrors.ErrInvalidArgument)
		}
		items, err := resources.ListCRDResources(ctx, principal, req.ClusterID, req.CRDName, req.Namespace)
		related["crdName"], related["count"] = req.CRDName, len(items)
		return items, related, err
	}
	if req.Namespace == "" {
		return nil, related, fmt.Errorf("%w: namespace is required", apperrors.ErrInvalidArgument)
	}
	if req.RangeMinutes == 0 {
		req.RangeMinutes = 15
	}
	if req.StepSeconds == 0 {
		req.StepSeconds = 60
	}
	if req.RangeMinutes < 1 || req.RangeMinutes > 1440 || req.StepSeconds < 1 || req.StepSeconds > 3600 {
		return nil, related, fmt.Errorf("%w: metric range or step exceeds limits", apperrors.ErrInvalidArgument)
	}
	switch name {
	case "k8s.pods.metrics":
		req.PodName = strings.TrimSpace(req.PodName)
		if req.PodName == "" {
			return nil, related, fmt.Errorf("%w: podName is required", apperrors.ErrInvalidArgument)
		}
		item, err := resources.GetPodMetrics(ctx, principal, req.ClusterID, req.Namespace, req.PodName, req.RangeMinutes, req.StepSeconds)
		related["podName"] = req.PodName
		return item, related, err
	case "k8s.deployments.metrics":
		req.DeploymentName = strings.TrimSpace(req.DeploymentName)
		if req.DeploymentName == "" {
			return nil, related, fmt.Errorf("%w: deploymentName is required", apperrors.ErrInvalidArgument)
		}
		item, err := resources.GetDeploymentMetrics(ctx, principal, req.ClusterID, req.Namespace, req.DeploymentName, req.RangeMinutes, req.StepSeconds)
		related["deploymentName"] = req.DeploymentName
		return item, related, err
	default:
		return nil, related, fmt.Errorf("%w: unknown Kubernetes diagnostic tool", apperrors.ErrInvalidArgument)
	}
}
