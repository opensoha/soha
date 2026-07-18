package resourcebackend

import (
	"context"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) ListHelmReleases(ctx context.Context, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := listHelmReleaseTableMetadata(queryCtx, bundle, namespace)
	if err != nil {
		if !metadataListUnsupported(err) {
			return nil, err
		}
		items, err = listHelmReleaseMetadataPages(queryCtx, bundle, namespace)
		if err != nil {
			return nil, err
		}
	}
	views := make([]domainresource.HelmReleaseView, 0, len(items))
	for _, item := range items {
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

func listHelmReleaseTableMetadata(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]metav1.PartialObjectMetadata, error) {
	var table metav1.Table
	err := bundle.Typed.CoreV1().RESTClient().Get().
		NamespaceIfScoped(namespace, strings.TrimSpace(namespace) != "").
		Resource("secrets").
		Param("labelSelector", "owner=helm").
		Param("includeObject", string(metav1.IncludeMetadata)).
		SetHeader("Accept", secretTableAcceptType).
		Do(ctx).
		Into(&table)
	if err != nil {
		return nil, err
	}
	items := make([]metav1.PartialObjectMetadata, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowAccessor(row)
		if err != nil {
			return nil, err
		}
		items = append(items, metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
			Name: metadata.GetName(), Namespace: metadata.GetNamespace(), Labels: metadata.GetLabels(), CreationTimestamp: metadata.GetCreationTimestamp(),
		}})
	}
	return items, nil
}

func listHelmReleaseMetadataPages(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]metav1.PartialObjectMetadata, error) {
	return listPartialMetadata(ctx, bundle, secretGVR, true, namespace, metav1.ListOptions{LabelSelector: "owner=helm"})
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
