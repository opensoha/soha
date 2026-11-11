package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
	helmreleasev1 "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kuberesource "k8s.io/cli-runtime/pkg/resource"
)

func requireInitialHelmResourcesAbsent(cfg *action.Configuration, input sohaapi.HelmExecutionTaskPayload) error {
	if input.Snapshot.Operation != sohaapi.HelmDeliverySnapshotOperationInstall || input.Snapshot.ExpectedRevision != 0 || input.Snapshot.RollbackRevision != 0 || input.Prepared == nil {
		return fmt.Errorf("%w: initialization requires a new install", ErrConflict)
	}
	for _, document := range append([]string{input.Prepared.Manifest}, input.Prepared.Hooks...) {
		resources, err := cfg.KubeClient.Build(strings.NewReader(document), false)
		if err != nil {
			return err
		}
		if err := requireHelmResourcesAbsent(resources); err != nil {
			return err
		}
	}
	return nil
}

func requireHelmResourcesAbsent(resources kube.ResourceList) error {
	for _, resource := range resources {
		err := resource.Get()
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: initialization resource exists or its absence cannot be verified", ErrConflict)
		}
	}
	return nil
}

// Helm's ordinary install may adopt correctly labelled existing objects. This
// client refuses that update path and forces native POSTs, including for hooks.
// Hook cleanup can delete only UIDs created by this exact native invocation.
type initializingHelmClient struct {
	kube.Interface
	mu      sync.Mutex
	created map[string]types.UID
	hooks   map[string]bool
}

func (c *initializingHelmClient) Create(resources kube.ResourceList, options ...kube.ClientCreateOption) (*kube.Result, error) {
	if err := requireHelmResourcesAbsent(resources); err != nil {
		return nil, err
	}
	result, err := c.Interface.Create(resources, append(options, kube.ClientCreateOptionServerSideApply(false, false))...)
	if result != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, resource := range result.Created {
			object, accessErr := meta.Accessor(resource.Object)
			if accessErr == nil && object.GetUID() != "" {
				key := initialHelmResourceKey(resource)
				c.created[key] = object.GetUID()
				if object.GetAnnotations()["helm.sh/hook"] != "" {
					if c.hooks == nil {
						c.hooks = make(map[string]bool)
					}
					c.hooks[key] = true
				}
			}
		}
	}
	return result, err
}

func (c *initializingHelmClient) Update(kube.ResourceList, kube.ResourceList, ...kube.ClientUpdateOption) (*kube.Result, error) {
	return nil, fmt.Errorf("%w: initialization cannot update or adopt existing resources", ErrConflict)
}

func (c *initializingHelmClient) Delete(resources kube.ResourceList, policy metav1.DeletionPropagation) (*kube.Result, []error) {
	result := &kube.Result{}
	for _, resource := range resources {
		if err := c.deleteCreatedHook(resource, policy); err != nil {
			return result, []error{err}
		}
		result.Deleted = append(result.Deleted, resource)
	}
	return result, nil
}

func (c *initializingHelmClient) deleteCreatedHook(resource *kuberesource.Info, policy metav1.DeletionPropagation) error {
	if err := resource.Get(); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("%w: hook identity cannot be verified", ErrConflict)
	}
	object, err := meta.Accessor(resource.Object)
	if err != nil {
		return err
	}
	c.mu.Lock()
	uid := c.created[initialHelmResourceKey(resource)]
	hook := c.hooks[initialHelmResourceKey(resource)]
	c.mu.Unlock()
	if !hook || uid == "" || object.GetUID() != uid || object.GetResourceVersion() == "" {
		return fmt.Errorf("%w: initialization cannot delete a pre-existing or replaced hook", ErrConflict)
	}
	version := object.GetResourceVersion()
	_, err = kuberesource.NewHelper(resource.Client, resource.Mapping).DeleteWithOptions(resource.Namespace, resource.Name, &metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	return err
}

func initialInventoryKey(item sohaapi.ManifestResourceInventory) string {
	group := ""
	if parts := strings.SplitN(item.APIVersion, "/", 2); len(parts) == 2 {
		group = parts[0]
	}
	return group + "/" + item.Kind + "/" + item.Namespace + "/" + item.Name
}

// ValidateInitializationResources binds a fresh observation to the durable
// original apply receipt, never to UIDs first read during recovery.
func ValidateInitializationResources(observed, original []sohaapi.ManifestResourceInventory) error {
	if len(original) == 0 || len(observed) != len(original) {
		return fmt.Errorf("%w: initialization creation receipt is missing or incomplete", ErrConflict)
	}
	uids := make(map[string]string, len(original))
	for _, item := range original {
		key := initialInventoryKey(item)
		if item.UID == "" || uids[key] != "" {
			return fmt.Errorf("%w: invalid initialization receipt", ErrConflict)
		}
		uids[key] = item.UID
	}
	for _, item := range observed {
		key := initialInventoryKey(item)
		if item.UID == "" || uids[key] != item.UID {
			return fmt.Errorf("%w: initialized resource was replaced", ErrConflict)
		}
		delete(uids, key)
	}
	return nil
}

func (c *initializingHelmClient) validateCreated(result sohaapi.HelmExecutionTaskResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(result.Resources) == 0 {
		return fmt.Errorf("%w: initialization has no observable resource receipt", ErrConflict)
	}
	for _, item := range result.Resources {
		if item.UID == "" || string(c.created[initialInventoryKey(item)]) != item.UID {
			return fmt.Errorf("%w: initialized resource creation UID cannot be verified", ErrConflict)
		}
	}
	return nil
}

func observeInitializedHelm(ctx context.Context, cfg *action.Configuration, input sohaapi.HelmExecutionTaskPayload, current *helmreleasev1.Release) (sohaapi.HelmExecutionTaskResult, error) {
	result, err := observeHelmDelivery(ctx, cfg, input.Snapshot, current)
	if err == nil && helmrelease.IsInitialization(input.Snapshot) {
		err = ValidateInitializationResources(result.Resources, input.InitializationResources)
	}
	if err != nil {
		result.Ready = false
	}
	return result, err
}

func initialHelmResourceKey(resource *kuberesource.Info) string {
	return resource.Mapping.GroupVersionKind.Group + "/" + resource.Mapping.GroupVersionKind.Kind + "/" + resource.Namespace + "/" + resource.Name
}

func (c *initializingHelmClient) GetWaiterWithOptions(strategy kube.WaitStrategy, options ...kube.WaitOption) (kube.Waiter, error) {
	if client, ok := c.Interface.(kube.InterfaceWaitOptions); ok {
		return client.GetWaiterWithOptions(strategy, options...)
	}
	return c.GetWaiter(strategy)
}
