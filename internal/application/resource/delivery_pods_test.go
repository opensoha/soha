package resource

import (
	"context"
	"errors"
	"slices"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	da "github.com/opensoha/soha/internal/domain/access"
	dc "github.com/opensoha/soha/internal/domain/cluster"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryPodTestBackend struct {
	DirectWorkloads
	onRead func()
}

func (deliveryPodTestBackend) ListDeliveryControllerPods(context.Context, dr.ResourceRef) ([]dr.PodView, error) {
	panic("the relation path must not use the typed native list")
}

func (b deliveryPodTestBackend) GetDeliveryPodStatus(_ context.Context, ref, _ dr.ResourceRef) (dr.PodView, error) {
	if b.onRead != nil {
		b.onRead()
	}
	return dr.PodView{Name: ref.Name, UID: ref.UID, Namespace: ref.Namespace}, nil
}

func (deliveryPodTestBackend) DecodeDeliveryPodStatus(content string, ref, _ dr.ResourceRef) (dr.PodView, error) {
	return dr.PodView{Name: ref.Name, UID: content, Namespace: ref.Namespace}, nil
}

type deliveryRelationTestSource struct {
	DirectGenericResource
	queries []cr.RelationQuery
	handoff bool
}

func (s *deliveryRelationTestSource) RelationSnapshot(_ context.Context, cluster string, input cr.RelationSnapshotRequest) (cr.RelationSnapshot, error) {
	s.queries = append(s.queries, input.Queries...)
	res := cr.RelationSnapshot{Version: cr.RelationSnapshotFeature}
	root := cr.RelationOwner{APIVersion: "example.com/v1", Kind: "Widget", Name: "root", UID: "root-uid", Controller: true}
	parent := cr.RelationOwner{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db", UID: "db-uid", Controller: true}
	for _, query := range input.Queries {
		item := relationFailure(query.ID, "available", "")
		add := func(name, uid string, owner cr.RelationOwner) {
			item.Objects = append(item.Objects, cr.RelationObject{
				Resource: cr.ResourceRef{ClusterID: cluster, APIVersion: query.Source.APIVersion, Kind: query.Source.Kind, Namespace: query.Source.Namespace, Name: name, UID: uid},
				Owners:   []cr.RelationOwner{owner},
			})
		}
		switch query.Source.Kind {
		case "StatefulSet":
			controller := root
			if s.handoff {
				controller.UID = "other-root"
			}
			add("db", "db-uid", controller)
		case "Pod":
			add("owned", "owned-uid", parent)
			stale := parent
			stale.UID = "db-replaced"
			add("broken-chain", "broken-uid", stale)
			reference := root
			reference.Controller = false
			add("owner-ref-without-control", "no-control-uid", reference)
			shared := parent
			shared.Name, shared.UID = "shared", "shared-uid"
			add("shared-operator", "shared-pod-uid", shared)
		}
		res.Sources = append(res.Sources, item)
	}
	return res, nil
}

func TestDeliveryResourcePodRelationsUseOnlyControllerChain(t *testing.T) {
	const content = `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"namespace":"team","name":"root","uid":"root-uid","generation":1}}`
	direct := &importSourceDirect{content: content}
	snapshot := &deliveryRelationTestSource{}
	permissions := exactPermissionAuthorizer{appaccess.PermPlatformExtensionsView: true, appaccess.PermPlatformDeploymentView: true, appaccess.PermPlatformPodsView: true}
	for _, kind := range []string{"ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob"} {
		permissions[appaccess.PlatformActionPermission("workloads", kind, "view")] = true
	}
	s := New(Dependencies{
		DirectCustom: direct, DirectGeneric: snapshot, DirectWorkloads: deliveryPodTestBackend{},
		Audit:       noopResourceAuditRecorder{},
		Connections: stubConnectionResolver{connection: dc.Connection{Summary: dc.Summary{ID: "cluster", ConnectionMode: dc.ConnectionModeDirectKubeconfig}}},
		Authorizer:  permissions,
	})
	source, err := s.Runtime().GetImportSource(t.Context(), di.Principal{}, "cluster", "team", "CustomResource", "root", "widgets.example.com")
	if err != nil {
		t.Fatal(err)
	}
	podSource := cr.DefaultRelationSources("team")[0]
	config := cr.RelationConfig{Version: "v1", RootUID: "root-uid", Sources: []cr.RelationSource{}, Rules: []cr.RelationRule{
		{ID: "shared", Type: "reference", Target: podSource, Name: "shared-operator"},
		{ID: "broken", Type: "manual", Target: podSource, Name: "broken-chain", UID: "broken-uid"},
	}}
	pods, err := s.Runtime().GetDeliveryWorkloadPods(t.Context(), di.Principal{}, source, config)
	if len(pods) != 1 || pods[0].UID != "owned-uid" || err != nil {
		t.Fatalf("controller-only projection=%+v, err=%v", pods, err)
	}
	if slices.ContainsFunc(snapshot.queries, func(query cr.RelationQuery) bool {
		return query.Source.Kind == "Secret" || (query.Name != "" && (query.Name != "db" || query.Source.Kind != "StatefulSet"))
	}) {
		t.Fatalf("configured relation rule or secret reached metadata lookup: %+v", snapshot.queries)
	}
	if !slices.ContainsFunc(snapshot.queries, func(query cr.RelationQuery) bool { return query.Name == "db" }) {
		t.Fatal("immediate owner was never rechecked by name")
	}
	s.Runtime().Workloads.direct = deliveryPodTestBackend{onRead: func() { snapshot.handoff = true }}
	pods, err = s.Runtime().GetDeliveryWorkloadPods(t.Context(), di.Principal{}, source, config)
	if len(pods) != 0 || !errors.Is(err, dr.ErrDeliveryPodOwnershipUnverified) {
		t.Fatalf("UID-stable intermediary handoff exposed Pods: %+v, err=%v", pods, err)
	}
}

type deliveryNamedAgentPod struct {
	WorkloadAgent
	requested string
}

func (a *deliveryNamedAgentPod) GetPodYAML(_ context.Context, namespace, name string) (dr.ResourceYAMLView, error) {
	a.requested = namespace + "/" + name
	return dr.ResourceYAMLView{Content: "rebuilt-pod-uid"}, nil
}

func TestDeliveryAgentPodStatusHonorsExactNameAndUID(t *testing.T) {
	agent := &deliveryNamedAgentPod{}
	s := New(Dependencies{
		Agents:      AgentClients{Workloads: func(dc.Connection) (WorkloadAgent, error) { return agent, nil }},
		Audit:       noopResourceAuditRecorder{},
		Connections: stubConnectionResolver{connection: dc.Connection{Summary: dc.Summary{ID: "cluster", ConnectionMode: dc.ConnectionModeAgent}}},
		Authorizer:  exactPermissionAuthorizer{"platform.pods.view": true},
	})
	proof := deliveryPodProof{pod: dr.ResourceRef{ClusterID: "cluster", Namespace: "team", Name: "pod", UID: "proof-pod-uid"}}
	pod, err := s.Runtime().readRelatedDeliveryPod(t.Context(), di.Principal{}, deliveryPodTestBackend{}, proof)
	if pod.UID != "" || !errors.Is(err, apperrors.ErrConflict) || agent.requested != "team/pod" {
		t.Fatalf("agent status from %q exposed replacement: %+v, err=%v", agent.requested, pod, err)
	}
	proof.pod.UID = "rebuilt-pod-uid"
	if pod, err := s.Runtime().readRelatedDeliveryPod(t.Context(), di.Principal{}, deliveryPodTestBackend{}, proof); pod.UID != proof.pod.UID || err != nil {
		t.Fatalf("agent status rejected proven UID: %+v, err=%v", pod, err)
	}
}

type deliveryPartialCollectionDeny struct{}

func (deliveryPartialCollectionDeny) Authorize(_ context.Context, request da.Request) (da.Decision, error) {
	if request.Resource.Kind == "Pod" && request.RequireWholeResourceCollection {
		return da.Decision{Reason: "policy denies individual pods in the collection"}, nil
	}
	return da.Decision{Allowed: true}, nil
}

func TestDeliveryNativePodsRespectScopedCollectionDeny(t *testing.T) {
	s := New(Dependencies{
		DirectWorkloads: deliveryPodTestBackend{},
		Audit:           noopResourceAuditRecorder{},
		Connections:     stubConnectionResolver{connection: dc.Connection{Summary: dc.Summary{ID: "cluster", ConnectionMode: dc.ConnectionModeDirectKubeconfig}}},
		Authorizer:      deliveryPartialCollectionDeny{},
	})
	source := dr.ResourceSourceSnapshot{Resource: dr.ResourceRef{ClusterID: "cluster", Namespace: "team", APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db"}}
	pods, err := s.Runtime().GetDeliveryWorkloadPods(t.Context(), di.Principal{}, source, cr.RelationConfig{})
	if len(pods) != 0 || !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("scope-specific deny allowed whole collection: %+v, err=%v", pods, err)
	}
}
