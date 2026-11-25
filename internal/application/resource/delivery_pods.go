package resource

import (
	"context"
	"errors"
	"fmt"
	"slices"

	cr "github.com/opensoha/soha/contracts/resource"
	da "github.com/opensoha/soha/internal/domain/access"
	dc "github.com/opensoha/soha/internal/domain/cluster"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sync/errgroup"
)

type deliveryPodBackend interface {
	ListDeliveryControllerPods(context.Context, dr.ResourceRef) ([]dr.PodView, error)
	GetDeliveryPodStatus(context.Context, dr.ResourceRef, dr.ResourceRef) (dr.PodView, error)
	DecodeDeliveryPodStatus(string, dr.ResourceRef, dr.ResourceRef) (dr.PodView, error)
}

// GetDeliveryWorkloadPods returns status only for Pods reached from a scoped
// workload through its current controller identities.
func (r *Runtime) GetDeliveryWorkloadPods(ctx context.Context, principal di.Principal, source dr.ResourceSourceSnapshot, config cr.RelationConfig) ([]dr.PodView, error) {
	ref := source.Resource
	if ref.ClusterID == "" || ref.Namespace == "" || len(ref.Name) > 253 || !relationObjectName.MatchString(ref.Name) {
		return nil, apperrors.ErrInvalidArgument
	}
	ctx = context.WithValue(r.WithDeliveryObservation(ctx), graphNamespacePolicyKey{}, true)
	backend, ok := r.Workloads.direct.(deliveryPodBackend)
	if !ok {
		return nil, apperrors.ErrUnsupportedOperation
	}
	if source.CRDName == "" {
		pods, err := r.nativeDeliveryPods(ctx, principal, backend, ref)
		if len(pods) > 0 || !errors.Is(err, apperrors.ErrUnsupportedOperation) {
			return pods, err
		}
	}
	return r.relatedDeliveryPods(ctx, principal, backend, source, config)
}

func (r *Runtime) nativeDeliveryPods(ctx context.Context, principal di.Principal, backend deliveryPodBackend, ref dr.ResourceRef) ([]dr.PodView, error) {
	w := r.Workloads
	named := graphResourceName(ctx, ref.Name)
	connection, _, err := w.authorizeDeliveryPodRead(named, principal, ref.ClusterID, ref.Namespace, ref.Kind, da.ActionView)
	if err != nil {
		return nil, err
	}
	if connection.Summary.ConnectionMode == dc.ConnectionModeAgent {
		// The current Agent Pod list reads a whole namespace; the metadata path
		// uses selectors and exact-name status reads instead.
		return nil, apperrors.ErrUnsupportedOperation
	}
	ctx = topologySourcePolicyContext(ctx, ref.Namespace, true)
	if ref.Kind == "Deployment" || ref.Kind == "CronJob" {
		kind, action := "ReplicaSet", da.ActionView
		if ref.Kind == "CronJob" {
			kind, action = "Job", da.ActionList
		}
		if _, _, err := w.authorizeDeliveryPodRead(ctx, principal, ref.ClusterID, ref.Namespace, kind, action); err != nil {
			return nil, err
		}
	}
	connection, _, err = w.authorizeDeliveryPodRead(ctx, principal, ref.ClusterID, ref.Namespace, "Pod", da.ActionList)
	if err != nil {
		return nil, err
	}
	pods, err := backend.ListDeliveryControllerPods(ctx, ref)
	if len(pods) > 0 {
		actions := w.allowedActionsForResource(ctx, principal, connection, ref.Namespace, "Pod", da.ActionList)
		for i := range pods {
			pods[i].AllowedActions = narrowAllowedActions(pods[i].AllowedActions, actions)
		}
	}
	if err == nil {
		_ = w.recordAudit(ctx, principal, ref.ClusterID, ref.Namespace, "Pod", "", string(da.ActionList), "success", "listed owned delivery pods")
	}
	return pods, err
}

func (w *Workloads) authorizeDeliveryPodRead(ctx context.Context, principal di.Principal, clusterID, namespace, kind string, action da.Action) (dc.Connection, da.Decision, error) {
	connection, decision, err := w.authorizeResourceGate(ctx, principal, clusterID, namespace, resourceGroupForKind(kind), kind, action)
	if err == nil && decision.ResourceScope != nil && decision.ResourceScope.LabelSelector != "" {
		err = apperrors.ErrAccessDenied
	}
	return connection, decision, err
}

func (r *Runtime) relatedDeliveryPods(ctx context.Context, principal di.Principal, backend deliveryPodBackend, source dr.ResourceSourceSnapshot, config cr.RelationConfig) ([]dr.PodView, error) {
	var selector map[string]string
	var err error
	if source.CRDName == "" {
		source, selector, err = r.deliveryRelationRoot(ctx, principal, source)
		if err != nil {
			return nil, err
		}
	}
	if config.Version != "" {
		if err := cr.ValidateRelationConfig(config); err != nil || config.RootUID != source.Resource.UID {
			return nil, apperrors.ErrConflict
		}
	}
	queries := deliveryPodRelationQueries(source, config.Sources, selector)
	view, err := r.getApplicationResourceRelations(ctx, principal, source, cr.RelationConfig{}, queries, true)
	if err != nil {
		return nil, err
	}
	parents := deliveryRelationParents(view)
	proofs, coverageErr := deliveryRelatedPodProofs(view, parents, len(queries), source.CRDName != "")
	pods, statusErr := r.readRelatedDeliveryPodStatus(ctx, principal, backend, proofs)
	if len(pods) == 0 {
		return pods, errors.Join(coverageErr, statusErr)
	}
	pods, freshnessErr := r.recheckDeliveryPodControllers(ctx, principal, source, queries, parents, pods)
	return pods, errors.Join(coverageErr, statusErr, freshnessErr)
}

func (r *Runtime) deliveryRelationRoot(ctx context.Context, principal di.Principal, expected dr.ResourceSourceSnapshot) (dr.ResourceSourceSnapshot, map[string]string, error) {
	ref := expected.Resource
	current, item, err := r.readImportSource(graphResourceName(ctx, ref.Name), principal, ref.ClusterID, ref.Namespace, ref.Kind, ref.Name, "")
	if err != nil {
		return current, nil, err
	}
	if ref.APIVersion != current.Resource.APIVersion || (ref.UID != "" && ref.UID != current.Resource.UID) {
		return current, nil, apperrors.ErrConflict
	}
	var object struct {
		Spec struct {
			Selector struct {
				MatchLabels map[string]string `yaml:"matchLabels"`
			} `yaml:"selector"`
		} `yaml:"spec"`
	}
	if yaml.Unmarshal([]byte(item.Content), &object) != nil {
		return current, nil, apperrors.ErrConflict
	}
	selector := object.Spec.Selector.MatchLabels
	if ref.Kind == "Job" && len(selector) == 0 {
		selector = map[string]string{"batch.kubernetes.io/job-name": ref.Name}
	}
	return current, selector, nil
}

func deliveryPodRelationQueries(source dr.ResourceSourceSnapshot, extra []cr.RelationSource, selector map[string]string) []cr.RelationQuery {
	ref := source.Resource
	var queries []cr.RelationQuery
	kinds := []string{"Pod"}
	if source.CRDName != "" {
		kinds = append(kinds, "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob")
	} else if ref.Kind == "CronJob" {
		kinds = append(kinds, "Job")
	} else if ref.Kind == "Deployment" {
		kinds = append(kinds, "ReplicaSet")
	}
	for _, item := range cr.DefaultRelationSources(ref.Namespace) {
		if slices.Contains(kinds, item.Kind) {
			queries = append(queries, cr.RelationQuery{ID: item.Resource, Source: item, Selector: selector})
		}
	}
	for _, item := range extra {
		if item.Namespace != ref.Namespace || cr.IsBuiltinRelationSource(item) || len(queries) >= cr.RelationMaxQueries {
			continue
		}
		queries = append(queries, cr.RelationQuery{ID: fmt.Sprintf("controller.%d", len(queries)), Source: item})
	}
	return queries
}

type deliveryPodProof struct {
	pod        dr.ResourceRef
	controller dr.ResourceRef
}

func deliveryRelationParents(view cr.ResourceRelationsView) map[string]dr.ResourceRef {
	nodes := make(map[string]dr.ResourceRef, len(view.Nodes))
	for _, node := range view.Nodes {
		nodes[node.Resource.UID] = node.Resource
	}
	parents := make(map[string]dr.ResourceRef)
	for _, edge := range view.Edges {
		if parent, ok := nodes[edge.FromUID]; edge.Type == "owns" && ok {
			if _, exists := parents[edge.ToUID]; exists {
				parents[edge.ToUID] = dr.ResourceRef{} // Multiple controller claims are ambiguous.
			} else {
				parents[edge.ToUID] = parent
			}
		}
	}
	return parents
}

func deliveryRelatedPodProofs(view cr.ResourceRelationsView, parents map[string]dr.ResourceRef, queryCount int, customRoot bool) ([]deliveryPodProof, error) {
	var proofs []deliveryPodProof
	coverageErr := deliveryRelationCoverageError(view, queryCount)
	for _, node := range view.Nodes {
		if node.Role != "owned" || node.Resource.Kind != "Pod" || node.Resource.APIVersion != "v1" {
			continue
		}
		if controller := parents[node.Resource.UID]; controller.UID != "" {
			proofs = append(proofs, deliveryPodProof{pod: node.Resource, controller: controller})
		} else {
			coverageErr = errors.Join(coverageErr, dr.ErrDeliveryPodOwnershipUnverified)
		}
	}
	if len(proofs) == 0 && customRoot && coverageErr == nil {
		coverageErr = dr.ErrDeliveryPodOwnershipUnverified
	}
	return proofs, coverageErr
}

func deliveryRelationCoverageError(view cr.ResourceRelationsView, queryCount int) error {
	var coverageErr error
	for _, coverage := range view.Coverage {
		switch coverage.State {
		case "available":
		case "denied":
			coverageErr = errors.Join(coverageErr, apperrors.ErrAccessDenied)
		case "unsupported":
			coverageErr = errors.Join(coverageErr, apperrors.ErrUnsupportedOperation)
		case "partial":
			coverageErr = errors.Join(coverageErr, dr.ErrDeliveryPodObservationLimit)
		default:
			coverageErr = errors.Join(coverageErr, apperrors.ErrClusterUnready)
		}
	}
	if len(view.Coverage) < queryCount {
		coverageErr = errors.Join(coverageErr, apperrors.ErrClusterUnready)
	}
	return coverageErr
}

const deliveryPodStatusLimit = 64

func (r *Runtime) readRelatedDeliveryPodStatus(ctx context.Context, principal di.Principal, backend deliveryPodBackend, proofs []deliveryPodProof) ([]dr.PodView, error) {
	var coverageErr error
	if len(proofs) > deliveryPodStatusLimit {
		proofs = proofs[:deliveryPodStatusLimit]
		coverageErr = dr.ErrDeliveryPodObservationLimit
	}
	var group errgroup.Group
	group.SetLimit(6)
	observed := make([]dr.PodView, len(proofs))
	readErrors := make([]error, len(proofs))
	for i, proof := range proofs {
		group.Go(func() error {
			observed[i], readErrors[i] = r.readRelatedDeliveryPod(ctx, principal, backend, proof)
			return nil
		})
	}
	_ = group.Wait()
	return slices.DeleteFunc(observed, func(pod dr.PodView) bool { return pod.UID == "" }), errors.Join(append(readErrors, coverageErr)...)
}

func (r *Runtime) readRelatedDeliveryPod(ctx context.Context, principal di.Principal, backend deliveryPodBackend, proof deliveryPodProof) (dr.PodView, error) {
	w := r.Workloads
	ref := proof.pod
	ctx = graphResourceName(ctx, ref.Name)
	connection, _, err := w.authorizeDeliveryPodRead(ctx, principal, ref.ClusterID, ref.Namespace, "Pod", da.ActionView)
	if err != nil {
		return dr.PodView{}, err
	}
	var pod dr.PodView
	if connection.Summary.ConnectionMode == dc.ConnectionModeAgent {
		route, err := w.routePods(connection, ref.ClusterID)
		if err != nil {
			return dr.PodView{}, err
		}
		item, err := route.GetPodYAML(ctx, ref.Namespace, ref.Name)
		if err != nil {
			return dr.PodView{}, route.RuntimeError(err)
		}
		pod, err = backend.DecodeDeliveryPodStatus(item.Content, ref, proof.controller)
		if err != nil {
			return dr.PodView{}, err
		}
	} else {
		pod, err = backend.GetDeliveryPodStatus(ctx, ref, proof.controller)
		if err != nil {
			return dr.PodView{}, err
		}
	}
	if pod.UID == "" || pod.UID != ref.UID || pod.Namespace != ref.Namespace || pod.Name != ref.Name {
		return dr.PodView{}, apperrors.ErrConflict
	}
	actionContext := topologySourcePolicyContext(ctx, ref.Namespace, true)
	pod.AllowedActions = narrowAllowedActions(pod.AllowedActions, w.allowedActionsForResource(actionContext, principal, connection, ref.Namespace, "Pod", da.ActionView))
	_ = w.recordAudit(ctx, principal, ref.ClusterID, ref.Namespace, "Pod", ref.Name, string(da.ActionView), "success", "viewed owned delivery pod status")
	return pod, nil
}

func (r *Runtime) recheckDeliveryPodControllers(ctx context.Context, principal di.Principal, source dr.ResourceSourceSnapshot, initial []cr.RelationQuery, parents map[string]dr.ResourceRef, pods []dr.PodView) ([]dr.PodView, error) {
	root := source.Resource.UID
	queries, coverageErr := deliveryIntermediaryQueries(initial, parents, pods, root)
	freshParents := map[string]dr.ResourceRef{}
	if len(queries) == 0 {
		if err := r.verifyRelationRoot(graphResourceName(ctx, source.Resource.Name), principal, source); err != nil {
			return nil, err
		}
	} else {
		view, err := r.getApplicationResourceRelations(ctx, principal, source, cr.RelationConfig{}, queries, true)
		if err != nil {
			return nil, err
		}
		freshParents = deliveryRelationParents(view)
		coverageErr = errors.Join(coverageErr, deliveryRelationCoverageError(view, len(queries)))
	}
	pods = slices.DeleteFunc(pods, func(pod dr.PodView) bool {
		controller := parents[pod.UID]
		if deliveryControllerReachesRoot(controller, freshParents, root) {
			return false
		}
		coverageErr = errors.Join(coverageErr, dr.ErrDeliveryPodOwnershipUnverified)
		return true
	})
	return pods, coverageErr
}

func deliveryIntermediaryQueries(initial []cr.RelationQuery, parents map[string]dr.ResourceRef, pods []dr.PodView, root string) ([]cr.RelationQuery, error) {
	var queries []cr.RelationQuery
	seen := make(map[string]bool)
	for _, pod := range pods {
		controller := parents[pod.UID]
		for depth := 0; depth < cr.RelationMaxDepth && controller.UID != "" && controller.UID != root && !seen[controller.UID]; depth++ {
			seen[controller.UID] = true
			index := slices.IndexFunc(initial, func(query cr.RelationQuery) bool {
				s := query.Source
				return s.APIVersion == controller.APIVersion && s.Kind == controller.Kind && s.Namespace == controller.Namespace
			})
			if index >= 0 {
				if len(queries) == cr.RelationMaxQueries {
					return queries, dr.ErrDeliveryPodObservationLimit
				}
				query := initial[index]
				query.ID, query.Name, query.Selector = fmt.Sprintf("current.%d", len(queries)), controller.Name, nil
				queries = append(queries, query)
			}
			controller = parents[controller.UID]
		}
	}
	return queries, nil
}

func deliveryControllerReachesRoot(controller dr.ResourceRef, parents map[string]dr.ResourceRef, root string) bool {
	for depth := 0; depth < cr.RelationMaxDepth && controller.UID != ""; depth++ {
		if controller.UID == root {
			return true
		}
		controller = parents[controller.UID]
	}
	return false
}
