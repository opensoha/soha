package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const agentResourceEventBuffer = 64

type agentEventInformer struct {
	apiVersion string
	kind       string
	namespaced bool
	informer   cache.SharedIndexInformer
	healthy    *atomic.Bool
}

type agentResourceSubscription struct {
	id        uint64
	namespace string
	kinds     map[string]struct{}
	events    chan domainresource.ResourceStreamEvent
}

type resourceEventStream struct {
	clusterID   string
	factory     informers.SharedInformerFactory
	informers   []agentEventInformer
	subscribers map[uint64]*agentResourceSubscription
	nextID      uint64
	mu          sync.RWMutex
	startOnce   sync.Once
	ready       atomic.Bool
	stopped     atomic.Bool
}

func newResourceEventStream(clusterID string, client kubernetes.Interface) *resourceEventStream {
	factory := informers.NewSharedInformerFactoryWithOptions(client, 2*time.Minute)
	stream := &resourceEventStream{
		clusterID: clusterID, factory: factory, subscribers: map[uint64]*agentResourceSubscription{},
		informers: []agentEventInformer{
			{"v1", "Namespace", false, factory.Core().V1().Namespaces().Informer(), nil},
			{"v1", "Node", false, factory.Core().V1().Nodes().Informer(), nil},
			{"v1", "Pod", true, factory.Core().V1().Pods().Informer(), nil},
			{"v1", "Service", true, factory.Core().V1().Services().Informer(), nil},
			{"v1", "Event", true, factory.Core().V1().Events().Informer(), nil},
			{"apps/v1", "Deployment", true, factory.Apps().V1().Deployments().Informer(), nil},
			{"apps/v1", "StatefulSet", true, factory.Apps().V1().StatefulSets().Informer(), nil},
			{"apps/v1", "DaemonSet", true, factory.Apps().V1().DaemonSets().Informer(), nil},
			{"apps/v1", "ReplicaSet", true, factory.Apps().V1().ReplicaSets().Informer(), nil},
			{"batch/v1", "Job", true, factory.Batch().V1().Jobs().Informer(), nil},
			{"batch/v1", "CronJob", true, factory.Batch().V1().CronJobs().Informer(), nil},
			{"networking.k8s.io/v1", "Ingress", true, factory.Networking().V1().Ingresses().Informer(), nil},
			{"discovery.k8s.io/v1", "EndpointSlice", true, factory.Discovery().V1().EndpointSlices().Informer(), nil},
			{"networking.k8s.io/v1", "NetworkPolicy", true, factory.Networking().V1().NetworkPolicies().Informer(), nil},
			{"v1", "ConfigMap", true, factory.Core().V1().ConfigMaps().Informer(), nil},
			{"v1", "Secret", true, factory.Core().V1().Secrets().Informer(), nil},
		},
	}
	for index := range stream.informers {
		stream.informers[index].healthy = &atomic.Bool{}
		stream.informers[index].healthy.Store(true)
	}
	return stream
}

func (s *resourceEventStream) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		for _, spec := range s.informers {
			spec := spec
			_ = spec.informer.SetTransform(func(obj any) (any, error) {
				return configurationCacheMetadata(obj), nil
			})
			_ = spec.informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
				// ponytail: after a watch failure, use live reads until restart.
				// HasSynced stays true during relists; per-object transforms cannot
				// prove that the replacement batch has reached the store. Rebuild
				// per-kind informers if automatic cache recovery becomes necessary.
				spec.healthy.Store(false)
				s.publish(domainresource.ResourceStreamEvent{Type: "error", ClusterID: s.clusterID, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: "agent-informer", CacheStatus: "degraded", Message: "agent informer watch failed", ResyncRequired: true})
			})
			if configurationCacheKind(spec.kind) {
				continue
			}
			_, _ = spec.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj any) { s.publishObject(spec, "added", obj) },
				UpdateFunc: func(oldObj, newObj any) {
					oldAccessor, oldErr := meta.Accessor(oldObj)
					newAccessor, newErr := meta.Accessor(newObj)
					if oldErr == nil && newErr == nil && oldAccessor.GetResourceVersion() != "" && oldAccessor.GetResourceVersion() == newAccessor.GetResourceVersion() {
						return
					}
					s.publishObject(spec, "modified", newObj)
				},
				DeleteFunc: func(obj any) { s.publishObject(spec, "deleted", agentDeletedObject(obj)) },
			})
		}
		s.factory.Start(ctx.Done())
		go func() {
			syncs := make([]cache.InformerSynced, 0, len(s.informers))
			for _, spec := range s.informers {
				if configurationCacheKind(spec.kind) {
					continue
				}
				syncs = append(syncs, spec.informer.HasSynced)
			}
			s.ready.Store(cache.WaitForCacheSync(ctx.Done(), syncs...))
			<-ctx.Done()
			s.stopped.Store(true)
			s.ready.Store(false)
		}()
	})
}

func (s *resourceEventStream) Ready() bool { return s != nil && s.ready.Load() }

func (s *resourceEventStream) Subscribe(namespace string, kinds []string) (<-chan domainresource.ResourceStreamEvent, func()) {
	filter := map[string]struct{}{}
	for _, kind := range kinds {
		if normalized := strings.ToLower(strings.TrimSpace(kind)); normalized != "" {
			filter[normalized] = struct{}{}
		}
	}
	s.mu.Lock()
	s.nextID++
	subscription := &agentResourceSubscription{id: s.nextID, namespace: strings.TrimSpace(namespace), kinds: filter, events: make(chan domainresource.ResourceStreamEvent, agentResourceEventBuffer)}
	s.subscribers[subscription.id] = subscription
	s.mu.Unlock()
	var once sync.Once
	return subscription.events, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subscribers, subscription.id)
			s.mu.Unlock()
		})
	}
}

func (s *resourceEventStream) publishObject(spec agentEventInformer, eventType string, obj any) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	scope := domainresource.ResourceScopeModeCluster
	if spec.namespaced {
		scope = domainresource.ResourceScopeModeNamespace
	}
	resource := &domainresource.ResourceRef{ClusterID: s.clusterID, APIVersion: spec.apiVersion, Kind: spec.kind, Name: accessor.GetName(), Namespace: accessor.GetNamespace(), ScopeMode: scope, UID: string(accessor.GetUID())}
	s.publish(domainresource.ResourceStreamEvent{Type: eventType, ClusterID: s.clusterID, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: "agent-informer", Resource: resource, ResourceVersion: accessor.GetResourceVersion(), CacheStatus: "live"})
}

func (s *resourceEventStream) publish(event domainresource.ResourceStreamEvent) {
	s.mu.RLock()
	subscribers := make([]*agentResourceSubscription, 0, len(s.subscribers))
	for _, subscription := range s.subscribers {
		subscribers = append(subscribers, subscription)
	}
	s.mu.RUnlock()
	for _, subscription := range subscribers {
		if !subscription.matches(event) {
			continue
		}
		select {
		case subscription.events <- event:
		default:
			select {
			case <-subscription.events:
			default:
			}
			select {
			case subscription.events <- domainresource.ResourceStreamEvent{Type: "reset", ClusterID: s.clusterID, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: "agent-informer", CacheStatus: "degraded", Message: "resource event buffer overflow", ResyncRequired: true}:
			default:
			}
		}
	}
}

func (s *agentResourceSubscription) matches(event domainresource.ResourceStreamEvent) bool {
	if event.Resource == nil {
		return true
	}
	if s.namespace != "" && event.Resource.Namespace != "" && event.Resource.Namespace != s.namespace {
		return false
	}
	if len(s.kinds) == 0 {
		return true
	}
	_, ok := s.kinds[strings.ToLower(event.Resource.Kind)]
	return ok
}

func agentDeletedObject(obj any) any {
	switch tombstone := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		return tombstone.Obj
	case *cache.DeletedFinalStateUnknown:
		return tombstone.Obj
	default:
		return obj
	}
}

func (c *Client) StartResourceEvents(ctx context.Context) {
	if c != nil && c.resourceEvents != nil {
		c.resourceEvents.Start(ctx)
	}
}

func (c *Client) SubscribeResourceEvents(namespace string, kinds []string) (<-chan domainresource.ResourceStreamEvent, func(), error) {
	if c == nil || c.resourceEvents == nil {
		return nil, nil, fmt.Errorf("resource event stream is unavailable")
	}
	events, cancel := c.resourceEvents.Subscribe(namespace, kinds)
	return events, cancel, nil
}

func (c *Client) ResourceEventsReady() bool {
	return c != nil && c.resourceEvents != nil && c.resourceEvents.Ready()
}
