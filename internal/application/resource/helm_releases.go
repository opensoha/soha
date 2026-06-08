package resource

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListHelmReleases(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.HelmReleaseView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHelmReleases(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectHelmReleases(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.HelmReleaseView) string { return item.Namespace })
	populateAllowedActionsHelmReleases(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed helm releases via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) listDirectHelmReleases(ctx context.Context, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	secrets, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HelmReleaseView, 0, len(secrets.Items))
	for _, item := range secrets.Items {
		views = append(views, mapHelmRelease(item.Name, item.Namespace, item.Labels, item.CreationTimestamp.Time, "secret"))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Namespace != views[j].Namespace {
			return views[i].Namespace < views[j].Namespace
		}
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].Revision > views[j].Revision
	})
	return dedupeHelmReleases(views), nil
}
func mapHelmRelease(name, namespace string, labels map[string]string, createdAt time.Time, storageDriver string) domainresource.HelmReleaseView {
	releaseName := strings.TrimSpace(labels["name"])
	if releaseName == "" {
		releaseName = parseHelmReleaseName(name)
	}
	revision := strings.TrimSpace(labels["version"])
	if revision == "" {
		revision = parseHelmRevision(name)
	}
	status := strings.TrimSpace(labels["status"])
	if status == "" {
		status = "unknown"
	}
	chart := strings.TrimSpace(labels["helm.sh/chart"])
	appVersion := strings.TrimSpace(labels["app.kubernetes.io/version"])
	return domainresource.HelmReleaseView{
		Name:          releaseName,
		Namespace:     namespace,
		Revision:      revision,
		Status:        status,
		Chart:         chart,
		AppVersion:    appVersion,
		StorageDriver: storageDriver,
		AgeSeconds:    secondsSince(createdAt),
	}
}
func populateAllowedActionsHelmReleases(items []domainresource.HelmReleaseView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func parseHelmReleaseName(name string) string {
	trimmed := strings.TrimPrefix(name, "sh.helm.release.v1.")
	if trimmed == name {
		return name
	}
	index := strings.LastIndex(trimmed, ".v")
	if index <= 0 {
		return trimmed
	}
	return trimmed[:index]
}
func parseHelmRevision(name string) string {
	index := strings.LastIndex(name, ".v")
	if index <= 0 {
		return ""
	}
	return name[index+2:]
}
func dedupeHelmReleases(items []domainresource.HelmReleaseView) []domainresource.HelmReleaseView {
	seen := make(map[string]struct{}, len(items))
	result := make([]domainresource.HelmReleaseView, 0, len(items))
	for _, item := range items {
		key := item.Namespace + "/" + item.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}
