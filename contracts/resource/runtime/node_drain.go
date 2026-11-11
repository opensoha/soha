package runtime

import (
	"context"
	"errors"
	"sync"

	"github.com/opensoha/soha-contracts/resource"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	policyclient "k8s.io/client-go/kubernetes/typed/policy/v1"
	policybetaclient "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
)

// DrainClient observes eviction failures while leaving kubectl drain semantics intact.
type DrainClient struct {
	kubernetes.Interface
	failures sync.Map
}

func NewDrainClient(client kubernetes.Interface) *DrainClient {
	return &DrainClient{Interface: client}
}

func (c *DrainClient) PolicyV1() policyclient.PolicyV1Interface {
	return drainPolicy{PolicyV1Interface: c.Interface.PolicyV1(), observer: c}
}

func (c *DrainClient) PolicyV1beta1() policybetaclient.PolicyV1beta1Interface {
	return drainBetaPolicy{PolicyV1beta1Interface: c.Interface.PolicyV1beta1(), observer: c}
}

func (c *DrainClient) observe(namespace, name string, err error) error {
	key := namespace + "/" + name
	if err == nil || apierrors.IsNotFound(err) {
		c.failures.Delete(key)
	} else {
		c.failures.Store(key, err)
	}
	return err
}

func (c *DrainClient) Failure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	code := "node_drain_failed"
	var denied error
	c.failures.Range(func(_, value any) bool {
		failure, ok := value.(error)
		if !ok {
			return true
		}
		switch {
		case apierrors.IsForbidden(failure) || apierrors.IsUnauthorized(failure):
			denied = failure
		case apierrors.HasStatusCause(failure, policyv1.DisruptionBudgetCause):
			code = "node_drain_pdb_blocked"
		case apierrors.IsTooManyRequests(failure) && code != "node_drain_pdb_blocked":
			code = "node_drain_eviction_blocked"
		}
		return true
	})
	if denied != nil {
		return denied
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		code = "node_drain_canceled"
	} else if code == "node_drain_failed" && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = "node_drain_timeout"
	}
	return &resource.NodeDrainError{Code: code, Cause: err}
}

type drainPolicy struct {
	policyclient.PolicyV1Interface
	observer *DrainClient
}

func (c *DrainClient) PodSelectionFailure(ctx context.Context, failures []error) error {
	for _, err := range failures {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) || apierrors.IsNotFound(err) {
			return err
		}
	}
	err := errors.Join(failures...)
	if ctx.Err() != nil {
		return c.Failure(ctx, err)
	}
	return &resource.NodeDrainError{Code: "node_drain_pod_selection", Cause: err}
}

func (c drainPolicy) Evictions(namespace string) policyclient.EvictionInterface {
	return drainEvictions{EvictionInterface: c.PolicyV1Interface.Evictions(namespace), observer: c.observer}
}

type drainEvictions struct {
	policyclient.EvictionInterface
	observer *DrainClient
}

func (c drainEvictions) Evict(ctx context.Context, eviction *policyv1.Eviction) error {
	return c.observer.observe(eviction.Namespace, eviction.Name, c.EvictionInterface.Evict(ctx, eviction))
}

type drainBetaPolicy struct {
	policybetaclient.PolicyV1beta1Interface
	observer *DrainClient
}

func (c drainBetaPolicy) Evictions(namespace string) policybetaclient.EvictionInterface {
	return drainBetaEvictions{EvictionInterface: c.PolicyV1beta1Interface.Evictions(namespace), observer: c.observer}
}

type drainBetaEvictions struct {
	policybetaclient.EvictionInterface
	observer *DrainClient
}

func (c drainBetaEvictions) Evict(ctx context.Context, eviction *policyv1beta1.Eviction) error {
	return c.observer.observe(eviction.Namespace, eviction.Name, c.EvictionInterface.Evict(ctx, eviction))
}
