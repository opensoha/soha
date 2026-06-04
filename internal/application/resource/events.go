package resource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
	informerinfra "github.com/soha/soha/internal/infrastructure/informer"
	"github.com/soha/soha/internal/platform/apperrors"
)

func (s *Service) ListClusterEvents(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Event", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ClusterEventView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListClusterEvents(ctx, namespace, limit)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectClusterEvents(ctx, clusterID, namespace, limit)
		if err != nil {
			return nil, err
		}
		items = rawItems
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ClusterEventView) string { return item.Namespace })
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Event", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed cluster events via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) listDirectClusterEvents(ctx context.Context, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, string, error) {
	var (
		rawItems []corev1.Event
		source   string
	)
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListEvents(clusterID, namespace); err == nil {
			rawItems = items
			source = "cache"
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		} else {
			bundle, err := s.clusters.Bundle(ctx, clusterID)
			if err != nil {
				return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
			}
			queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			items, err := bundle.Typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, "live", err
			}
			rawItems = items.Items
			source = "live"
		}
	} else {
		bundle, err := s.clusters.Bundle(ctx, clusterID)
		if err != nil {
			return nil, "live", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		items, err := bundle.Typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, "live", err
		}
		rawItems = items.Items
		source = "live"
	}
	views := make([]domainresource.ClusterEventView, 0, len(rawItems))
	for _, item := range rawItems {
		views = append(views, mapClusterEvent(item))
	}
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].LastTimestamp > views[j].LastTimestamp
	})
	if limit > 0 && len(views) > limit {
		views = views[:limit]
	}
	return views, source, nil
}
func mapClusterEvent(item corev1.Event) domainresource.ClusterEventView {
	last := item.LastTimestamp.Time
	if last.IsZero() {
		last = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	return domainresource.ClusterEventView{Name: item.Name, Namespace: item.Namespace, Type: item.Type, Reason: item.Reason, InvolvedKind: item.InvolvedObject.Kind, InvolvedName: item.InvolvedObject.Name, Message: item.Message, Count: item.Count, LastTimestamp: last.UTC().Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}
