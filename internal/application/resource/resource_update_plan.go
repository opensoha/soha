package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	if err := validateResourceYAMLNamespace(request.Kind, request.Namespace); err != nil {
		return domainoperation.Plan{}, err
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, request.Namespace, request.Kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	if err := g.authorizeResourceYAMLData(ctx, principal, request.Kind); err != nil {
		return domainoperation.Plan{}, err
	}
	var analysis domainresource.ResourceUpdateAnalysis
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, clientErr := g.genericResourceAgentClient(connection)
		if clientErr != nil {
			return domainoperation.Plan{}, clientErr
		}
		if request.RequireObservedIdentity {
			guarded, ok := client.(observedYAMLAgent)
			if !ok {
				return domainoperation.Plan{}, apperrors.ErrUnsupportedOperation
			}
			analysis, err = guarded.DryRunObservedResourceYAML(ctx, request.Namespace, request.Kind, request.Name, request.Content)
		} else {
			analysis, err = client.DryRunResourceYAML(ctx, request.Namespace, request.Kind, request.Name, request.Content)
		}
		if err != nil {
			return domainoperation.Plan{}, wrapAgentPreflightError(err)
		}
	} else if g.direct == nil {
		return domainoperation.Plan{}, fmt.Errorf("%w: direct generic resource adapter is not configured", apperrors.ErrClusterUnready)
	} else if request.RequireObservedIdentity {
		guarded, ok := g.direct.(observedYAMLDirect)
		if !ok {
			return domainoperation.Plan{}, apperrors.ErrUnsupportedOperation
		}
		analysis, err = guarded.DryRunObservedResourceYAML(ctx, clusterID, request.Namespace, request.Kind, request.Name, request.Content)
	} else {
		analysis, err = g.direct.DryRunResourceYAML(ctx, clusterID, request.Namespace, request.Kind, request.Name, request.Content)
	}
	if err != nil {
		return domainoperation.Plan{}, err
	}
	return resourceUpdatePlan(connection.Summary.ID, request, analysis, request)
}

func resourceUpdatePlan(clusterID string, request domainresource.ResourceUpdatePlanRequest, analysis domainresource.ResourceUpdateAnalysis, input any) (domainoperation.Plan, error) {
	encoded, err := json.Marshal(input)
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
	target := strings.Join([]string{clusterID, request.Namespace, request.Kind, request.Name}, "/")
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

func wrapAgentPreflightError(err error) error {
	classified := wrapAgentResourceError(err)
	if errors.Is(classified, apperrors.ErrClusterUnready) && !errors.Is(err, apperrors.ErrClusterUnready) {
		return fmt.Errorf("%w: %w", apperrors.ErrClusterUnready, err)
	}
	return classified
}

func resourceUpdateRisk(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "secret", "namespace", "node", "persistentvolume", "storageclass", "role", "clusterrole", "rolebinding", "clusterrolebinding", "mutatingwebhookconfiguration", "validatingwebhookconfiguration":
		return "high", true
	default:
		return "mutate", false
	}
}
