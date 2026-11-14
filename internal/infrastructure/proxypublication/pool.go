// Package proxypublication adapts installed endpoint policy and current
// deployment membership for the publication application.
package proxypublication

import (
	"context"
	"fmt"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	proxyDomain "github.com/opensoha/soha/internal/domain/networkproxy"
	deploymentDomain "github.com/opensoha/soha/internal/domain/proxydeployment"
	publicationDomain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
	"slices"
)

type PoolReader interface {
	ApprovedPool(context.Context, string) (deploymentDomain.ApprovedPool, error)
}
type InstanceReader interface {
	Get(context.Context, string) (proxyDomain.Instance, error)
}
type Pool struct {
	deployment PoolReader
	instances  InstanceReader
	slots      []config.ProxyRuntimeSlotConfig
}

func (p Pool) ApprovedPool(ctx context.Context, id string) (publicationDomain.ApprovedPool, error) {
	pool, err := p.deployment.ApprovedPool(ctx, id)
	if err != nil {
		return publicationDomain.ApprovedPool{}, err
	}
	if pool.UID != "" && (pool.DesiredReplicas < 0 || pool.DesiredReplicas > pool.MaxReplicas || (deploymentDomain.Slot{Members: pool.Members}).MemberSetHash() != pool.MemberSetHash) {
		return publicationDomain.ApprovedPool{}, apperrors.ErrServiceUnavailable
	}
	out := publicationDomain.ApprovedPool{MasterID: pool.MasterID, OwnerEpoch: pool.OwnerEpoch, MemberSetHash: pool.MemberSetHash, Members: []publicationDomain.PoolMember{}, Endpoints: []api.NetworkProxyEmployeeEndpoint{}}
	for _, member := range pool.Members {
		// RuntimeID is the certificate identity and ProxyInstance primary key;
		// InstanceID is a Pod/process name used only by deployment control.
		item, err := p.instances.Get(ctx, member.RuntimeID)
		if err != nil {
			return out, err
		}
		running, serving := memberAvailability(pool, member.Ordinal)
		mapped := publicationDomain.PoolMember{RuntimeID: member.RuntimeID, ExpectedRevision: item.DesiredRevision, Running: running, Serving: serving}
		if !running {
			mapped.ServingUntil = pool.ServingUntil
		}
		out.Members = append(out.Members, mapped)
	}
	if pool.UID == "" {
		return out, nil
	}
	endpoints, err := p.endpoints(pool)
	if err != nil {
		return out, err
	}
	out.Endpoints = endpoints
	return out, nil
}
func (p Pool) endpoints(pool deploymentDomain.ApprovedPool) ([]api.NetworkProxyEmployeeEndpoint, error) {
	var matched *config.ProxyRuntimeSlotConfig
	for i := range p.slots {
		slot := &p.slots[i]
		if slot.ProxyID != pool.MasterID || slot.UID != pool.UID || slot.Slot().MemberSetHash() != pool.MemberSetHash {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("%w: ambiguous proxy pool endpoint policy", apperrors.ErrServiceUnavailable)
		}
		matched = slot
	}
	if matched == nil {
		return nil, fmt.Errorf("%w: proxy pool has no installed endpoint policy", apperrors.ErrServiceUnavailable)
	}
	out := make([]api.NetworkProxyEmployeeEndpoint, 0, len(matched.Endpoints))
	for _, endpoint := range matched.Endpoints {
		out = append(out, api.NetworkProxyEmployeeEndpoint{EndpointID: endpoint.EndpointID, URL: endpoint.URL})
	}
	return out, nil
}

func NewPool(deployment PoolReader, instances InstanceReader, slots []config.ProxyRuntimeSlotConfig) (*Pool, error) {
	for _, dependency := range []any{deployment, instances} {
		if dependency == nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		value := reflect.ValueOf(dependency)
		switch value.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
			if value.IsNil() {
				return nil, apperrors.ErrServiceUnavailable
			}
		}
	}
	policies := slices.Clone(slots)
	for i := range policies {
		policies[i].Members = slices.Clone(policies[i].Members)
		policies[i].Endpoints = slices.Clone(policies[i].Endpoints)
	}
	return &Pool{deployment: deployment, instances: instances, slots: policies}, nil
}

// Desired replicas govern new publication work. During a normal drain, observed
// replicas can still carry existing connections until their original lease ends.
func memberAvailability(pool deploymentDomain.ApprovedPool, ordinal int32) (bool, bool) {
	if pool.UID == "" || pool.Owner != "soha-managed" || ordinal < 0 {
		return false, false
	}
	switch pool.Phase {
	case "queued", "reconciling", "resuming", "converged":
	default:
		return false, false
	}
	running := ordinal < pool.DesiredReplicas
	if pool.ObservedReplicas == nil || *pool.ObservedReplicas < 0 || *pool.ObservedReplicas > pool.MaxReplicas {
		return running, false
	}
	serving := ordinal < *pool.ObservedReplicas
	if !pool.DrainHeld {
		serving = serving && running
	}
	return running, serving
}
