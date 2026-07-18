package resourcebackend

import (
	"context"
	"fmt"
	"sort"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Direct struct {
	clusters *Clusters
	cache    *Cache
}

func NewDirect(clusters *Clusters, cache *Cache) *Direct {
	return &Direct{clusters: clusters, cache: cache}
}

func (d *Direct) ListClusterEvents(ctx context.Context, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, string, error) {
	var (
		rawItems []corev1.Event
		source   string
	)
	if d.cache != nil {
		if items, err := d.cache.ListEvents(clusterID, namespace); err == nil {
			rawItems = items
			source = "cache"
		} else if !d.cache.CacheUnavailable(err) {
			return nil, "cache", err
		}
	}
	if source == "" {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, "live", err
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

func (d *Direct) directClients(ctx context.Context, clusterID string) (*k8sinfra.Bundle, error) {
	if d == nil || d.clusters == nil {
		return nil, fmt.Errorf("%w: direct cluster provider is not configured", apperrors.ErrClusterUnready)
	}
	bundle, err := d.clusters.manager.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	return bundle, nil
}

func mapClusterEvent(item corev1.Event) domainresource.ClusterEventView {
	last := item.LastTimestamp.Time
	if last.IsZero() {
		last = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	return domainresource.ClusterEventView{
		Name: item.Name, Namespace: item.Namespace, Type: item.Type, Reason: item.Reason,
		InvolvedKind: item.InvolvedObject.Kind, InvolvedName: item.InvolvedObject.Name,
		Message: item.Message, Count: item.Count, LastTimestamp: last.UTC().Format(time.RFC3339),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func secondsSince(timestamp time.Time) int64 {
	return int64(time.Since(timestamp).Seconds())
}

var _ appresource.DirectEventReader = (*Direct)(nil)
