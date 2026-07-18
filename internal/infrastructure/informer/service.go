package informer

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/redaction"
)

var ErrCacheNotReady = errors.New("informer cache not ready")

const (
	resourceNamespaces   = "namespaces"
	resourceNodes        = "nodes"
	resourcePods         = "pods"
	resourceServices     = "services"
	resourceEvents       = "events"
	resourceDeployments  = "deployments"
	resourceStatefulSets = "statefulsets"
	resourceIngresses    = "ingresses"
)

var informerResources = []string{
	resourceNamespaces, resourceNodes, resourcePods, resourceServices,
	resourceEvents, resourceDeployments, resourceStatefulSets, resourceIngresses,
}

type bundleManager interface {
	ClusterIDs() []string
	Bundle(context.Context, string) (*k8sinfra.Bundle, error)
}

type resourceState struct {
	mu             sync.RWMutex
	ready          bool
	status         string
	message        string
	lastTransition time.Time
}

func newResourceState() *resourceState {
	return &resourceState{status: "warming", lastTransition: time.Now().UTC()}
}

func (s *resourceState) transition(status, message string, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != status || s.message != message || s.ready != ready {
		s.lastTransition = time.Now().UTC()
	}
	s.status = status
	s.message = message
	s.ready = ready
}

func (s *resourceState) diagnostic(resource string) domaincluster.CacheResourceDiagnostic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return domaincluster.CacheResourceDiagnostic{
		Resource: resource, Status: s.status, Ready: s.ready,
		Message: s.message, LastTransition: s.lastTransition,
	}
}

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
	resources         map[string]*resourceState
}

type Service struct {
	manager            bundleManager
	caches             map[string]*clusterCache
	registrationErrors map[string]domaincluster.CacheDiagnostic
	mu                 sync.RWMutex
}

func New(manager bundleManager) *Service {
	return &Service{
		manager: manager, caches: map[string]*clusterCache{},
		registrationErrors: map[string]domaincluster.CacheDiagnostic{},
	}
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
		s.recordRegistrationError(clusterID, err)
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
		resources:         newResourceStates(),
	}
	informers := []struct {
		resource string
		informer cache.SharedIndexInformer
	}{
		{resourceNamespaces, nsInformer.Informer()},
		{resourceNodes, nodeInformer.Informer()},
		{resourcePods, podInformer.Informer()},
		{resourceServices, serviceInformer.Informer()},
		{resourceEvents, eventInformer.Informer()},
		{resourceDeployments, deployInformer.Informer()},
		{resourceStatefulSets, statefulSetInformer.Informer()},
		{resourceIngresses, ingressInformer.Informer()},
	}
	for _, item := range informers {
		state := entry.resources[item.resource]
		if err := item.informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
			state.transition("degraded", redaction.Text(err.Error()), state.diagnostic("").Ready)
		}); err != nil {
			state.transition("error", redaction.Text(err.Error()), false)
		}
	}

	s.mu.Lock()
	if _, exists := s.caches[clusterID]; exists {
		s.mu.Unlock()
		close(entry.stopCh)
		return nil
	}
	s.caches[clusterID] = entry
	delete(s.registrationErrors, clusterID)
	s.mu.Unlock()

	factory.Start(entry.stopCh)
	for _, item := range informers {
		go s.waitForSync(entry, item.resource, item.informer.HasSynced)
	}
	return nil
}

func newResourceStates() map[string]*resourceState {
	states := make(map[string]*resourceState, len(informerResources))
	for _, resource := range informerResources {
		states[resource] = newResourceState()
	}
	return states
}

func (s *Service) UnregisterCluster(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.caches[clusterID]; ok {
		close(entry.stopCh)
		delete(s.caches, clusterID)
	}
	delete(s.registrationErrors, clusterID)
}

func (s *Service) waitForSync(entry *clusterCache, resource string, syncFn cache.InformerSynced) {
	if cache.WaitForCacheSync(entry.stopCh, syncFn) {
		entry.resources[resource].transition("ready", "", true)
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
	if !ok {
		return false
	}
	for _, resource := range informerResources {
		if !entry.resources[resource].diagnostic(resource).Ready {
			return false
		}
	}
	return true
}

func (s *Service) Status(clusterID string) domaincluster.CacheDiagnostic {
	entry, ok := s.entry(clusterID)
	if !ok {
		s.mu.RLock()
		status, failed := s.registrationErrors[clusterID]
		s.mu.RUnlock()
		if failed {
			return status
		}
		return domaincluster.CacheDiagnostic{Status: "disabled", Message: "informer cache is not registered"}
	}
	resources := make([]domaincluster.CacheResourceDiagnostic, 0, len(informerResources))
	readyCount := 0
	degraded := false
	for _, resource := range informerResources {
		diagnostic := entry.resources[resource].diagnostic(resource)
		resources = append(resources, diagnostic)
		if diagnostic.Ready {
			readyCount++
		}
		if diagnostic.Status == "degraded" || diagnostic.Status == "error" {
			degraded = true
		}
	}
	status := "warming"
	if readyCount == len(resources) && !degraded {
		status = "ready"
	} else if readyCount > 0 || degraded {
		status = "partial"
	}
	return domaincluster.CacheDiagnostic{Status: status, Ready: readyCount == len(resources), Resources: resources}
}

func (s *Service) recordRegistrationError(clusterID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrationErrors[clusterID] = domaincluster.CacheDiagnostic{Status: "error", Message: redaction.Text(err.Error())}
}

func (s *Service) resourceReady(entry *clusterCache, resource string) bool {
	return entry.resources[resource].diagnostic(resource).Ready
}

// List methods return shallow value snapshots of client-go's immutable cache
// objects. Consumers must map them to response models without mutating nested fields.
func (s *Service) ListNamespaces(clusterID string) ([]corev1.Namespace, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceNamespaces) {
		return nil, ErrCacheNotReady
	}
	items, err := entry.namespaceLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list namespaces from cache: %w", err)
	}
	out := make([]corev1.Namespace, 0, len(items))
	for _, item := range items {
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListNodes(clusterID string) ([]corev1.Node, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceNodes) {
		return nil, ErrCacheNotReady
	}
	items, err := entry.nodeLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list nodes from cache: %w", err)
	}
	out := make([]corev1.Node, 0, len(items))
	for _, item := range items {
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListPods(clusterID, namespace string) ([]corev1.Pod, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourcePods) {
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
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListServices(clusterID, namespace string) ([]corev1.Service, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceServices) {
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
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListDeployments(clusterID, namespace string) ([]appsv1.Deployment, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceDeployments) {
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
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListStatefulSets(clusterID, namespace string) ([]appsv1.StatefulSet, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceStatefulSets) {
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
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListIngresses(clusterID, namespace string) ([]networkingv1.Ingress, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceIngresses) {
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
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) ListEvents(clusterID, namespace string) ([]corev1.Event, error) {
	entry, ok := s.entry(clusterID)
	if !ok || !s.resourceReady(entry, resourceEvents) {
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
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) entry(clusterID string) (*clusterCache, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.caches[clusterID]
	return entry, ok
}
