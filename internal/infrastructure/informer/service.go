package informer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	networkinglisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"

	k8sinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
)

var ErrCacheNotReady = errors.New("informer cache not ready")

type clusterCache struct {
	factory           informers.SharedInformerFactory
	namespaceLister   corelisters.NamespaceLister
	nodeLister        corelisters.NodeLister
	podLister         corelisters.PodLister
	serviceLister     corelisters.ServiceLister
	eventLister       corelisters.EventLister
	deployLister      appslisters.DeploymentLister
	statefulSetLister appslisters.StatefulSetLister
	ingressLister     networkinglisters.IngressLister
	stopCh            chan struct{}
	ready             atomic.Bool
}

type Service struct {
	manager *k8sinfra.Manager
	caches  map[string]*clusterCache
	mu      sync.RWMutex
}

func New(manager *k8sinfra.Manager) *Service {
	return &Service{manager: manager, caches: map[string]*clusterCache{}}
}

func (s *Service) Start(ctx context.Context) error {
	for _, clusterID := range s.manager.ClusterIDs() {
		if err := s.RegisterCluster(ctx, clusterID); err != nil {
			continue
		}
	}
	return nil
}

func (s *Service) RegisterCluster(ctx context.Context, clusterID string) error {
	s.mu.RLock()
	_, exists := s.caches[clusterID]
	s.mu.RUnlock()
	if exists {
		return nil
	}

	bundle, err := s.manager.Bundle(ctx, clusterID)
	if err != nil {
		return err
	}
	factory := informers.NewSharedInformerFactoryWithOptions(bundle.Typed, 2*time.Minute)
	nsInformer := factory.Core().V1().Namespaces()
	nodeInformer := factory.Core().V1().Nodes()
	podInformer := factory.Core().V1().Pods()
	serviceInformer := factory.Core().V1().Services()
	eventInformer := factory.Core().V1().Events()
	deployInformer := factory.Apps().V1().Deployments()
	statefulSetInformer := factory.Apps().V1().StatefulSets()
	ingressInformer := factory.Networking().V1().Ingresses()
	entry := &clusterCache{
		factory:           factory,
		namespaceLister:   nsInformer.Lister(),
		nodeLister:        nodeInformer.Lister(),
		podLister:         podInformer.Lister(),
		serviceLister:     serviceInformer.Lister(),
		eventLister:       eventInformer.Lister(),
		deployLister:      deployInformer.Lister(),
		statefulSetLister: statefulSetInformer.Lister(),
		ingressLister:     ingressInformer.Lister(),
		stopCh:            make(chan struct{}),
	}

	s.mu.Lock()
	if _, exists := s.caches[clusterID]; exists {
		s.mu.Unlock()
		close(entry.stopCh)
		return nil
	}
	s.caches[clusterID] = entry
	s.mu.Unlock()

	factory.Start(entry.stopCh)
	go s.waitForSync(
		entry,
		nsInformer.Informer().HasSynced,
		nodeInformer.Informer().HasSynced,
		podInformer.Informer().HasSynced,
		serviceInformer.Informer().HasSynced,
		eventInformer.Informer().HasSynced,
		deployInformer.Informer().HasSynced,
		statefulSetInformer.Informer().HasSynced,
		ingressInformer.Informer().HasSynced,
	)
	return nil
}

func (s *Service) UnregisterCluster(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.caches[clusterID]; ok {
		close(entry.stopCh)
		delete(s.caches, clusterID)
	}
}

func (s *Service) waitForSync(entry *clusterCache, syncFns ...cache.InformerSynced) {
	done := make(chan bool, 1)
	go func() {
		done <- cache.WaitForCacheSync(entry.stopCh, syncFns...)
	}()

	select {
	case synced := <-done:
		entry.ready.Store(synced)
	case <-time.After(15 * time.Second):
		entry.ready.Store(false)
		go func() {
			if synced := <-done; synced {
				entry.ready.Store(true)
			}
		}()
	}
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for clusterID, entry := range s.caches {
		close(entry.stopCh)
		delete(s.caches, clusterID)
	}
}

func (s *Service) Ready(clusterID string) bool {
	entry, ok := s.entry(clusterID)
	return ok && entry.ready.Load()
}

func (s *Service) ListNamespaces(clusterID string) ([]corev1.Namespace, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	items, err := entry.namespaceLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list namespaces from cache: %w", err)
	}
	out := make([]corev1.Namespace, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListNodes(clusterID string) ([]corev1.Node, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	items, err := entry.nodeLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list nodes from cache: %w", err)
	}
	out := make([]corev1.Node, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListPods(clusterID, namespace string) ([]corev1.Pod, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	var (
		items []*corev1.Pod
		err   error
	)
	if namespace == "" {
		items, err = entry.podLister.List(labels.Everything())
	} else {
		items, err = entry.podLister.Pods(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("list pods from cache: %w", err)
	}
	out := make([]corev1.Pod, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListServices(clusterID, namespace string) ([]corev1.Service, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	var (
		items []*corev1.Service
		err   error
	)
	if namespace == "" {
		items, err = entry.serviceLister.List(labels.Everything())
	} else {
		items, err = entry.serviceLister.Services(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("list services from cache: %w", err)
	}
	out := make([]corev1.Service, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListDeployments(clusterID, namespace string) ([]appsv1.Deployment, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	var (
		items []*appsv1.Deployment
		err   error
	)
	if namespace == "" {
		items, err = entry.deployLister.List(labels.Everything())
	} else {
		items, err = entry.deployLister.Deployments(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("list deployments from cache: %w", err)
	}
	out := make([]appsv1.Deployment, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListStatefulSets(clusterID, namespace string) ([]appsv1.StatefulSet, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	var (
		items []*appsv1.StatefulSet
		err   error
	)
	if namespace == "" {
		items, err = entry.statefulSetLister.List(labels.Everything())
	} else {
		items, err = entry.statefulSetLister.StatefulSets(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("list statefulsets from cache: %w", err)
	}
	out := make([]appsv1.StatefulSet, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListIngresses(clusterID, namespace string) ([]networkingv1.Ingress, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	var (
		items []*networkingv1.Ingress
		err   error
	)
	if namespace == "" {
		items, err = entry.ingressLister.List(labels.Everything())
	} else {
		items, err = entry.ingressLister.Ingresses(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("list ingresses from cache: %w", err)
	}
	out := make([]networkingv1.Ingress, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) ListEvents(clusterID, namespace string) ([]corev1.Event, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !entry.ready.Load() {
		return nil, ErrCacheNotReady
	}
	var (
		items []*corev1.Event
		err   error
	)
	if namespace == "" {
		items, err = entry.eventLister.List(labels.Everything())
	} else {
		items, err = entry.eventLister.Events(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("list events from cache: %w", err)
	}
	out := make([]corev1.Event, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func (s *Service) entry(clusterID string) (*clusterCache, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.caches[clusterID]
	return entry, ok
}
