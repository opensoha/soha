package resource

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	da "github.com/opensoha/soha/internal/domain/access"
	dc "github.com/opensoha/soha/internal/domain/cluster"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Match Kubernetes API object subdomain names without importing a transport package.
// Like IsDNS1123Subdomain, the caller limits the whole name to 253 characters.
var relationObjectName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

type relationDirect interface {
	RelationSnapshot(context.Context, string, cr.RelationSnapshotRequest) (cr.RelationSnapshot, error)
}
type relationAgent interface {
	RelationSnapshot(context.Context, cr.RelationSnapshotRequest) (cr.RelationSnapshot, error)
}

func (r *Runtime) GetApplicationResourceRelations(ctx context.Context, p di.Principal, source dr.ResourceSourceSnapshot, config cr.RelationConfig) (cr.ResourceRelationsView, error) {
	if config.Version == "" {
		config = cr.RelationConfig{Version: "v1", RootUID: source.Resource.UID, Sources: []cr.RelationSource{}, Rules: []cr.RelationRule{}}
	}
	if err := cr.ValidateRelationConfig(config); err != nil {
		return cr.ResourceRelationsView{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	if config.RootUID != source.Resource.UID {
		return cr.ResourceRelationsView{}, fmt.Errorf("%w: relation rules belong to a replaced root", apperrors.ErrConflict)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.verifyRelationRoot(ctx, p, source); err != nil {
		return cr.ResourceRelationsView{}, err
	}
	queries := relationQueries(source, config)
	allowed := cr.RelationSnapshotRequest{}
	snapshot := cr.RelationSnapshot{Version: cr.RelationSnapshotFeature, Sources: []cr.RelationSourceResult{}}
	var connection dc.Connection
	for _, query := range queries {
		current, err := r.authorizeRelationQuery(ctx, p, source.Resource.ClusterID, query)
		if err != nil {
			snapshot.Sources = append(snapshot.Sources, relationFailure(query.ID, "denied", "source_permission"))
			continue
		}
		connection = current
		allowed.Queries = append(allowed.Queries, query)
	}
	if len(allowed.Queries) > 0 {
		readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
		read, err := r.readRelationSnapshot(readCtx, connection, allowed)
		readCancel()
		if err != nil {
			state, reason := "error", "source_unavailable"
			if errors.Is(err, apperrors.ErrAccessDenied) {
				state, reason = "denied", "action_not_allowed"
			}
			if errors.Is(err, cr.ErrRelationSnapshotUnsupported) {
				state, reason = "unsupported", "agent_upgrade_required"
			}
			for _, query := range allowed.Queries {
				snapshot.Sources = append(snapshot.Sources, relationFailure(query.ID, state, reason))
			}
		} else {
			snapshot.Sources = append(snapshot.Sources, sanitizeRelationSnapshot(read, allowed, source.Resource.ClusterID).Sources...)
		}
	}
	if err := r.verifyRelationRoot(ctx, p, source); err != nil {
		return cr.ResourceRelationsView{}, err
	}
	view := dr.BuildResourceRelations(source.Resource, config, snapshot)
	_ = r.Workloads.recordAudit(ctx, p, source.Resource.ClusterID, source.Resource.Namespace, source.Resource.Kind, source.Resource.Name, "view", "success", "viewed linked resource metadata relationships")
	return view, nil
}

func (r *Runtime) verifyRelationRoot(ctx context.Context, p di.Principal, expected dr.ResourceSourceSnapshot) error {
	kind := expected.Resource.Kind
	if expected.CRDName != "" {
		kind = "CustomResource"
	}
	current, err := r.GetImportSource(ctx, p, expected.Resource.ClusterID, expected.Resource.Namespace, kind, expected.Resource.Name, expected.CRDName)
	if err != nil {
		return err
	}
	if current.Resource.UID != expected.Resource.UID || current.Resource.APIVersion != expected.Resource.APIVersion || current.Resource.Kind != expected.Resource.Kind {
		return fmt.Errorf("%w: root resource identity changed", apperrors.ErrConflict)
	}
	return nil
}

func relationQueries(source dr.ResourceSourceSnapshot, config cr.RelationConfig) []cr.RelationQuery {
	sources := cr.DefaultRelationSources(source.Resource.Namespace)
	if source.Plural != "" {
		sources = append(sources, cr.RelationSource{APIVersion: source.Resource.APIVersion, Kind: source.Resource.Kind, Resource: source.Plural, Namespace: source.Resource.Namespace})
	}
	sources = append(sources, config.Sources...)
	queries := []cr.RelationQuery{}
	seen := map[cr.RelationSource]bool{}
	for _, item := range sources {
		if seen[item] {
			continue
		}
		seen[item] = true
		queries = append(queries, cr.RelationQuery{ID: fmt.Sprintf("owner.%d", len(queries)), Source: item})
	}
	for _, rule := range config.Rules {
		queries = append(queries, cr.RelationQuery{ID: "rule." + rule.ID, Source: rule.Target, Name: rule.Name, Selector: rule.Selector})
	}
	return queries
}

func (r *Runtime) authorizeRelationQuery(ctx context.Context, p di.Principal, clusterID string, q cr.RelationQuery) (dc.Connection, error) {
	if r.generic == nil {
		return dc.Connection{}, apperrors.ErrUnsupportedOperation
	}
	action := da.ActionList
	if q.Name != "" {
		action = da.ActionView
	}
	group := resourceGroupForKind(q.Source.Kind)
	if !cr.IsBuiltinRelationSource(q.Source) {
		group = "extensions"
	}
	connection, decision, err := r.generic.authorizeResourceGroup(ctx, p, clusterID, q.Source.Namespace, group, q.Source.Kind, action)
	if err != nil {
		return connection, err
	}
	if decision.ResourceScope != nil && decision.ResourceScope.LabelSelector != "" {
		return connection, apperrors.ErrAccessDenied
	}
	if cr.IsBuiltinRelationSource(q.Source) {
		return connection, nil
	}
	custom := r.customResources
	if custom == nil {
		return connection, apperrors.ErrUnsupportedOperation
	}
	if _, err = custom.authorizeCRDDefinitionAccess(ctx, p, clusterID, da.ActionView); err != nil {
		return connection, err
	}
	parts := strings.SplitN(q.Source.APIVersion, "/", 2)
	if len(parts) != 2 {
		return connection, apperrors.ErrAccessDenied
	}
	definition, err := custom.resolveCRDResourceDefinition(ctx, connection, q.Source.Resource+"."+parts[0])
	if err != nil {
		return connection, err
	}
	if !definition.Namespaced || definition.Group != parts[0] || definition.Resource != q.Source.Resource || definition.Kind != q.Source.Kind {
		return connection, apperrors.ErrAccessDenied
	}
	if !slices.Contains(configuredCustomResourceActions(connection, definition, q.Source.Namespace, []string{string(action)}), string(action)) {
		return connection, apperrors.ErrAccessDenied
	}
	return connection, nil
}

func (r *Runtime) readRelationSnapshot(ctx context.Context, connection dc.Connection, input cr.RelationSnapshotRequest) (cr.RelationSnapshot, error) {
	if connection.Summary.ConnectionMode == dc.ConnectionModeAgent {
		client, err := r.generic.genericResourceAgentClient(connection)
		if err != nil {
			return cr.RelationSnapshot{}, err
		}
		reader, ok := client.(relationAgent)
		if !ok {
			return cr.RelationSnapshot{}, cr.ErrRelationSnapshotUnsupported
		}
		return reader.RelationSnapshot(ctx, input)
	}
	reader, ok := r.generic.direct.(relationDirect)
	if !ok {
		return cr.RelationSnapshot{}, cr.ErrRelationSnapshotUnsupported
	}
	return reader.RelationSnapshot(ctx, connection.Summary.ID, input)
}

func relationFailure(id, state, reason string) cr.RelationSourceResult {
	return cr.RelationSourceResult{Coverage: cr.RelationCoverage{QueryID: id, State: state, ReasonCode: reason}, Objects: []cr.RelationObject{}}
}

func sanitizeRelationSnapshot(input cr.RelationSnapshot, request cr.RelationSnapshotRequest, clusterID string) cr.RelationSnapshot {
	out := cr.RelationSnapshot{Version: cr.RelationSnapshotFeature, Sources: []cr.RelationSourceResult{}}
	for _, q := range request.Queries {
		result := relationFailure(q.ID, "error", "invalid_snapshot")
		if input.Version == cr.RelationSnapshotFeature {
			for _, source := range input.Sources {
				if source.Coverage.QueryID == q.ID {
					result = source
					break
				}
			}
		}
		if !slices.Contains([]string{"available", "partial", "denied", "error", "not_installed", "unsupported"}, result.Coverage.State) {
			result = relationFailure(q.ID, "error", "invalid_snapshot")
		}
		objects := []cr.RelationObject{}
		if result.Coverage.State == "available" || result.Coverage.State == "partial" {
			for i, item := range result.Objects {
				if i >= cr.RelationMaxItems {
					result.Coverage.State = "partial"
					result.Coverage.Truncated = true
					break
				}
				if validRelationObject(item, q, clusterID) {
					item.Resource.ScopeMode = cr.ResourceScopeModeNamespace
					objects = append(objects, item)
				} else {
					result.Coverage.State = "partial"
					result.Coverage.ReasonCode = "invalid_identity"
				}
			}
		}
		result.Objects = objects
		result.Coverage.ReasonCode = cleanRelationReason(result.Coverage.ReasonCode)
		out.Sources = append(out.Sources, result)
	}
	return out
}

func cleanRelationReason(reason string) string {
	if slices.Contains([]string{"", "invalid_snapshot", "invalid_identity", "kubernetes_rbac", "source_not_served", "source_unavailable", "invalid_response", "item_budget", "byte_budget", "action_not_allowed", "timeout", "cancelled"}, reason) {
		return reason
	}
	return "source_unavailable"
}

func validRelationObject(item cr.RelationObject, q cr.RelationQuery, clusterID string) bool {
	if !validRelationIdentity(item.Resource, q, clusterID) || len(item.Owners) > 32 {
		return false
	}
	for _, owner := range item.Owners {
		if owner.UID == "" || len(owner.UID) > 128 || owner.Name == "" || len(owner.Name) > 253 || owner.Kind == "" || owner.APIVersion == "" {
			return false
		}
	}
	return true
}

func validRelationIdentity(ref cr.ResourceRef, q cr.RelationQuery, clusterID string) bool {
	if ref.ClusterID != clusterID || ref.APIVersion != q.Source.APIVersion || ref.Kind != q.Source.Kind || ref.Namespace != q.Source.Namespace {
		return false
	}
	return ref.UID != "" && len(ref.UID) <= 128 && len(ref.Name) <= 253 && relationObjectName.MatchString(ref.Name) && (q.Name == "" || ref.Name == q.Name)
}
