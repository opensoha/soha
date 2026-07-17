package resource

import (
	"context"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ResourceRisk string

const (
	ResourceRiskNone                  ResourceRisk = "none"
	ResourceRiskAccessEscalation      ResourceRisk = "access_escalation"
	ResourceRiskClusterInfrastructure ResourceRisk = "cluster_infrastructure"
	ResourceRiskExtensionDefinition   ResourceRisk = "extension_definition"
	ResourceRiskAdmissionControl      ResourceRisk = "admission_control"
)

type HighRiskResourceRequest struct {
	APIVersion string
	Group      string
	Resource   string
	Kind       string
	ClusterID  string
	Namespace  string
	Object     map[string]any
}

type HighRiskClassification struct {
	Risk                ResourceRisk
	RequiredPermissions []string
	Namespaced          bool
}

type HighRiskResourcePolicy struct {
	permissions RuntimePermissionAuthorizer
}

func NewHighRiskResourcePolicy(permissions RuntimePermissionAuthorizer) *HighRiskResourcePolicy {
	return &HighRiskResourcePolicy{permissions: permissions}
}

// ClassifyHighRiskResource trusts a discovery-resolved group/resource when it
// is available. Kind is a conservative fallback for legacy callers that have
// not resolved the manifest through discovery yet.
func ClassifyHighRiskResource(group, resource, kind string) HighRiskClassification {
	group = strings.ToLower(strings.TrimSpace(group))
	resource = strings.ToLower(strings.TrimSpace(resource))
	if resource == "" {
		return classifyHighRiskKind(kind)
	}
	switch group + "/" + resource {
	case "rbac.authorization.k8s.io/roles":
		return highRisk(ResourceRiskAccessEscalation, true, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACEscalate)
	case "rbac.authorization.k8s.io/clusterroles":
		return highRisk(ResourceRiskAccessEscalation, false, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACEscalate)
	case "rbac.authorization.k8s.io/rolebindings":
		return highRisk(ResourceRiskAccessEscalation, true, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACBind)
	case "rbac.authorization.k8s.io/clusterrolebindings":
		return highRisk(ResourceRiskAccessEscalation, false, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACBind)
	case "/namespaces":
		return highRisk(ResourceRiskClusterInfrastructure, false, appaccess.PermPlatformNamespacesManage)
	case "apiextensions.k8s.io/customresourcedefinitions":
		return highRisk(ResourceRiskExtensionDefinition, false, appaccess.PermPlatformCRDsManage)
	case "admissionregistration.k8s.io/mutatingwebhookconfigurations", "admissionregistration.k8s.io/validatingwebhookconfigurations":
		return highRisk(ResourceRiskAdmissionControl, false, appaccess.PermPlatformAdmissionManage)
	case "storage.k8s.io/storageclasses", "scheduling.k8s.io/priorityclasses":
		return highRisk(ResourceRiskClusterInfrastructure, false, appaccess.PermPlatformClusterResourcesManage)
	default:
		return HighRiskClassification{Risk: ResourceRiskNone}
	}
}

func classifyHighRiskKind(kind string) HighRiskClassification {
	switch normalizeResourceKind(kind) {
	case "role":
		return highRisk(ResourceRiskAccessEscalation, true, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACEscalate)
	case "clusterrole":
		return highRisk(ResourceRiskAccessEscalation, false, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACEscalate)
	case "rolebinding":
		return highRisk(ResourceRiskAccessEscalation, true, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACBind)
	case "clusterrolebinding":
		return highRisk(ResourceRiskAccessEscalation, false, appaccess.PermPlatformRBACManage, appaccess.PermPlatformRBACBind)
	case "namespace":
		return highRisk(ResourceRiskClusterInfrastructure, false, appaccess.PermPlatformNamespacesManage)
	case "customresourcedefinition":
		return highRisk(ResourceRiskExtensionDefinition, false, appaccess.PermPlatformCRDsManage)
	case "mutatingwebhookconfiguration", "validatingwebhookconfiguration":
		return highRisk(ResourceRiskAdmissionControl, false, appaccess.PermPlatformAdmissionManage)
	case "storageclass", "priorityclass":
		return highRisk(ResourceRiskClusterInfrastructure, false, appaccess.PermPlatformClusterResourcesManage)
	default:
		return HighRiskClassification{Risk: ResourceRiskNone}
	}
}

func (p *HighRiskResourcePolicy) Check(ctx context.Context, principal domainidentity.Principal, request HighRiskResourceRequest) error {
	classification := ClassifyHighRiskResource(request.Group, request.Resource, request.Kind)
	if classification.Risk == ResourceRiskNone {
		return nil
	}
	if err := validateHighRiskNamespace(request, classification); err != nil {
		return err
	}
	if p == nil || p.permissions == nil {
		return highRiskPermissionError(request.Kind, classification.Risk, "permission resolver unavailable")
	}
	for _, permission := range classification.RequiredPermissions {
		if err := p.permissions.Authorize(ctx, principal, permission); err != nil {
			return highRiskPermissionError(request.Kind, classification.Risk, "missing permission "+permission)
		}
	}
	return nil
}

func highRisk(risk ResourceRisk, namespaced bool, permissions ...string) HighRiskClassification {
	return HighRiskClassification{
		Risk:                risk,
		RequiredPermissions: append([]string(nil), permissions...),
		Namespaced:          namespaced,
	}
}

func normalizeResourceKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func validateHighRiskNamespace(request HighRiskResourceRequest, classification HighRiskClassification) error {
	if !classification.Namespaced {
		return nil
	}
	target := strings.TrimSpace(request.Namespace)
	if target == "" {
		return fmt.Errorf("%w: namespace_required: %s requires a target namespace", apperrors.ErrInvalidArgument, strings.TrimSpace(request.Kind))
	}
	manifestNamespace := nestedString(request.Object, "metadata", "namespace")
	if manifestNamespace != "" && manifestNamespace != target {
		return fmt.Errorf("%w: namespace_mismatch: manifest namespace %s does not match authorized target %s", apperrors.ErrAccessDenied, manifestNamespace, target)
	}
	if isNamespacedRoleBinding(request) {
		roleRefNamespace := nestedString(request.Object, "roleRef", "namespace")
		if roleRefNamespace != "" && roleRefNamespace != target {
			return fmt.Errorf("%w: namespace_mismatch: RoleBinding cannot reference Role namespace %s from target %s", apperrors.ErrAccessDenied, roleRefNamespace, target)
		}
	}
	return nil
}

func isNamespacedRoleBinding(request HighRiskResourceRequest) bool {
	resource := strings.ToLower(strings.TrimSpace(request.Resource))
	if resource != "" {
		return strings.EqualFold(strings.TrimSpace(request.Group), "rbac.authorization.k8s.io") && resource == "rolebindings"
	}
	return normalizeResourceKind(request.Kind) == "rolebinding"
}

func nestedString(object map[string]any, path ...string) string {
	var current any = object
	for _, key := range path {
		values, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = values[key]
		if !ok {
			return ""
		}
	}
	value, _ := current.(string)
	return strings.TrimSpace(value)
}

func highRiskPermissionError(kind string, risk ResourceRisk, reason string) error {
	return fmt.Errorf("%w: high_risk_permission_required: %s (%s): %s", apperrors.ErrAccessDenied, strings.TrimSpace(kind), risk, reason)
}
