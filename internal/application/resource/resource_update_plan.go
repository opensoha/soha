package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (g *GenericResources) PlanResourceYAMLUpdate(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.ResourceUpdatePlanRequest) (domainoperation.Plan, error) {
	request.Namespace = strings.TrimSpace(request.Namespace)
	request.Kind = strings.TrimSpace(request.Kind)
	request.Name = strings.TrimSpace(request.Name)
	if request.Kind == "" || request.Name == "" || strings.TrimSpace(request.Content) == "" {
		return domainoperation.Plan{}, fmt.Errorf("%w: kind, name, and content are required", apperrors.ErrInvalidArgument)
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, request.Namespace, request.Kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	var analysis domainresource.ResourceUpdateAnalysis
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := g.genericResourceAgentClient(connection)
		if err != nil {
			return domainoperation.Plan{}, err
		}
		analysis, err = client.DryRunResourceYAML(ctx, request.Namespace, request.Kind, request.Name, request.Content)
	} else if g.direct == nil {
		return domainoperation.Plan{}, fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
	} else {
		analysis, err = g.direct.DryRunResourceYAML(ctx, clusterID, request.Namespace, request.Kind, request.Name, request.Content)
	}
	if err != nil {
		return domainoperation.Plan{}, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return domainoperation.Plan{}, fmt.Errorf("hash resource update plan: %w", err)
	}
	digest := sha256.Sum256(encoded)
	risk, approval := resourceUpdateRisk(request.Kind)
	sensitive := strings.EqualFold(request.Kind, "Secret")
	warnings := []string{"Kubernetes API server-side dry-run passed; no resource was changed."}
	ready := len(analysis.Conflicts) == 0
	if !ready {
		warnings = append(warnings, "Server-side apply detected field ownership conflicts; the update is blocked until the manifest is reconciled with the current owners.")
	}
	if sensitive {
		warnings = append(warnings, "Secret values are redacted from the change summary.")
	}
	target := strings.Join([]string{connection.Summary.ID, request.Namespace, request.Kind, request.Name}, "/")
	return domainoperation.Plan{
		Capability: "k8s.resources.update", Target: target, Ready: ready,
		RiskLevel: risk, RequiresApproval: approval, InputHash: hex.EncodeToString(digest[:]),
		Changes: []domainoperation.PlanChange{{
			Action: "update", Resource: request.Kind + "/" + request.Name,
			Summary: "apply the reviewed manifest to the selected resource", SensitiveValuesRedacted: sensitive,
		}},
		Warnings: warnings, KubernetesResourceUpdate: &analysis,
	}, nil
}

func resourceUpdateRisk(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "secret", "namespace", "node", "persistentvolume", "storageclass", "role", "clusterrole", "rolebinding", "clusterrolebinding", "mutatingwebhookconfiguration", "validatingwebhookconfiguration":
		return "high", true
	default:
		return "mutate", false
	}
}
