package resourcebackend

import (
	"context"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) ListHelmReleases(ctx context.Context, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	secrets, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HelmReleaseView, 0, len(secrets.Items))
	for _, item := range secrets.Items {
		views = append(views, mapHelmRelease(item.Name, item.Namespace, item.Labels, item.CreationTimestamp.Time))
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

func mapHelmRelease(name, namespace string, labels map[string]string, createdAt time.Time) domainresource.HelmReleaseView {
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
	return domainresource.HelmReleaseView{
		Name: releaseName, Namespace: namespace, Revision: revision, Status: status,
		Chart:         strings.TrimSpace(labels["helm.sh/chart"]),
		AppVersion:    strings.TrimSpace(labels["app.kubernetes.io/version"]),
		StorageDriver: "secret", AgeSeconds: secondsSince(createdAt),
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

var _ appresource.DirectHelmReleaseReader = (*Direct)(nil)
