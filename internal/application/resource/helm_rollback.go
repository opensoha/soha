package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *Helm) GetHelmReleaseManifest(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revision string) (domainresource.HelmReleaseManifestView, error) {
	connection, _, err := h.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return domainresource.HelmReleaseManifestView{}, err
	}
	if err := h.authorizeRuntimePermission(ctx, principal, appaccess.PermPlatformHelmValuesView); err != nil {
		return domainresource.HelmReleaseManifestView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := h.helmAgentClient(connection)
		if err != nil {
			return domainresource.HelmReleaseManifestView{}, err
		}
		return client.GetHelmReleaseManifest(ctx, namespace, name, revision)
	}
	return h.direct.GetHelmReleaseManifest(ctx, clusterID, namespace, name, revision)
}

func (h *Helm) PlanHelmReleaseRollback(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, input domainresource.HelmReleaseRollbackInput) (domainoperation.Plan, error) {
	namespace, name, input, err := normalizeHelmRollbackRequest(namespace, name, input)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	connection, _, err := h.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionRollback)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := h.helmAgentClient(connection)
		if err != nil {
			return domainoperation.Plan{}, err
		}
		err = client.DryRunHelmReleaseRollback(ctx, namespace, name, input)
	} else {
		err = h.direct.DryRunHelmReleaseRollback(ctx, clusterID, namespace, name, input)
	}
	if err != nil {
		return domainoperation.Plan{}, err
	}
	encoded, err := json.Marshal(struct {
		ClusterID string                                  `json:"clusterId"`
		Namespace string                                  `json:"namespace"`
		Name      string                                  `json:"name"`
		Input     domainresource.HelmReleaseRollbackInput `json:"input"`
	}{connection.Summary.ID, namespace, name, input})
	if err != nil {
		return domainoperation.Plan{}, fmt.Errorf("hash helm rollback plan: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return domainoperation.Plan{
		Capability: "k8s.helm.releases.rollback", Target: strings.Join([]string{connection.Summary.ID, namespace, name}, "/"),
		Ready: true, RiskLevel: "high", RequiresApproval: true, InputHash: hex.EncodeToString(digest[:]),
		Changes:  []domainoperation.PlanChange{{Action: "rollback", Resource: "HelmRelease/" + name, Summary: fmt.Sprintf("rollback to revision %d", input.Revision)}},
		Warnings: []string{"Helm server-side dry-run passed; workload resources were not changed."},
	}, nil
}

func (h *Helm) RollbackHelmRelease(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, input domainresource.HelmReleaseRollbackInput) (domainresource.HelmReleaseDetailView, error) {
	namespace, name, input, err := normalizeHelmRollbackRequest(namespace, name, input)
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}
	connection, _, err := h.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionRollback)
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}
	var item domainresource.HelmReleaseDetailView
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := h.helmAgentClient(connection)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, err
		}
		item, err = client.RollbackHelmRelease(ctx, namespace, name, input)
	} else {
		item, err = h.direct.RollbackHelmRelease(ctx, clusterID, namespace, name, input)
	}
	if err != nil {
		_ = h.recordAudit(ctx, principal, clusterID, namespace, "HelmRelease", name, string(domainaccess.ActionRollback), "failure", err.Error())
		return domainresource.HelmReleaseDetailView{}, err
	}
	_ = h.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionRollback), "success", fmt.Sprintf("rolled back helm release to revision %d", input.Revision))
	h.recordOperation(ctx, principal, "platform.helm.release.rollback", connection.Summary.ID, namespace, "HelmRelease", name, fmt.Sprintf("rolled back to revision %d", input.Revision), map[string]any{"revision": input.Revision})
	return item, nil
}

func normalizeHelmRollbackRequest(namespace, name string, input domainresource.HelmReleaseRollbackInput) (string, string, domainresource.HelmReleaseRollbackInput, error) {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" {
		return namespace, name, input, fmt.Errorf("%w: namespace and release name are required", apperrors.ErrInvalidArgument)
	}
	input, err := normalizeHelmRollbackInput(input)
	return namespace, name, input, err
}

func normalizeHelmRollbackInput(input domainresource.HelmReleaseRollbackInput) (domainresource.HelmReleaseRollbackInput, error) {
	if input.Revision < 1 {
		return input, fmt.Errorf("%w: revision must be a positive integer", apperrors.ErrInvalidArgument)
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 300
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 3600 {
		return input, fmt.Errorf("%w: timeoutSeconds must be between 1 and 3600", apperrors.ErrInvalidArgument)
	}
	return input, nil
}
