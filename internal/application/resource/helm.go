package resource

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	helmrelease "github.com/kubecrux/kubecrux/internal/platform/helmrelease"
)

type helmReleaseRecord struct {
	createdAt time.Time
	labels    map[string]string
	release   *helmrelease.Release
	secret    string
}

func (s *Service) GetHelmReleaseDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.HelmReleaseDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}

	var (
		item   domainresource.HelmReleaseDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, err
		}
		item, err = client.GetHelmReleaseDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectHelmReleaseDetail(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, err
		}
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	item.ValuesEditable = false
	item.ValuesDiffEnabled = true
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed helm release detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ListHelmReleaseHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.HelmReleaseHistoryView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.HelmReleaseHistoryView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHelmReleaseHistory(ctx, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectHelmReleaseHistory(ctx, clusterID, namespace, name)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	for index := range items {
		items[index].AllowedActions = stringifyActions(decision.AllowedActions)
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionView), "success", fmt.Sprintf("listed helm release history via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) GetHelmReleaseValues(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revision string) (domainresource.HelmValuesView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}

	var (
		item   domainresource.HelmValuesView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.HelmValuesView{}, err
		}
		item, err = client.GetHelmReleaseValues(ctx, namespace, name, revision)
		if err != nil {
			return domainresource.HelmValuesView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectHelmReleaseValues(ctx, clusterID, namespace, name, revision)
		if err != nil {
			return domainresource.HelmValuesView{}, err
		}
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	item.Editable = false
	item.DiffEnabled = true
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed helm release values via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) getDirectHelmReleaseDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.HelmReleaseDetailView, error) {
	record, err := s.getDirectHelmReleaseRecord(ctx, clusterID, namespace, name, "")
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}
	return mapHelmReleaseDetail(record), nil
}

func (s *Service) listDirectHelmReleaseHistory(ctx context.Context, clusterID, namespace, name string) ([]domainresource.HelmReleaseHistoryView, error) {
	records, err := s.listDirectHelmReleaseRecords(ctx, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	filtered := make([]domainresource.HelmReleaseHistoryView, 0)
	for _, record := range records {
		if record.release == nil {
			continue
		}
		if record.release.Name != name {
			continue
		}
		filtered = append(filtered, mapHelmReleaseHistory(record))
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		leftRevision, _ := strconv.Atoi(filtered[i].Revision)
		rightRevision, _ := strconv.Atoi(filtered[j].Revision)
		return leftRevision > rightRevision
	})
	if len(filtered) == 0 {
		return nil, fmt.Errorf("%w: helm release %s not found", apperrors.ErrNotFound, name)
	}
	return filtered, nil
}

func (s *Service) getDirectHelmReleaseValues(ctx context.Context, clusterID, namespace, name, revision string) (domainresource.HelmValuesView, error) {
	record, err := s.getDirectHelmReleaseRecord(ctx, clusterID, namespace, name, revision)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	content, err := helmrelease.ValuesYAML(record.release)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	return domainresource.HelmValuesView{
		Name:        record.release.Name,
		Namespace:   record.release.Namespace,
		Revision:    strconv.Itoa(record.release.Version),
		Content:     content,
		Original:    content,
		Editable:    false,
		DiffEnabled: true,
	}, nil
}

func (s *Service) getDirectHelmReleaseRecord(ctx context.Context, clusterID, namespace, name, revision string) (helmReleaseRecord, error) {
	records, err := s.listDirectHelmReleaseRecords(ctx, clusterID, namespace)
	if err != nil {
		return helmReleaseRecord{}, err
	}
	for _, record := range records {
		if record.release == nil {
			continue
		}
		if record.release.Name != name {
			continue
		}
		if revision != "" && strconv.Itoa(record.release.Version) != revision {
			continue
		}
		return record, nil
	}
	return helmReleaseRecord{}, fmt.Errorf("%w: helm release %s not found", apperrors.ErrNotFound, name)
}

func (s *Service) listDirectHelmReleaseRecords(ctx context.Context, clusterID, namespace string) ([]helmReleaseRecord, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	secrets, err := bundle.Typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}
	records := make([]helmReleaseRecord, 0, len(secrets.Items))
	for _, item := range secrets.Items {
		releaseData := strings.TrimSpace(string(item.Data["release"]))
		if releaseData == "" {
			continue
		}
		release, err := helmrelease.Decode(releaseData, item.Labels)
		if err != nil {
			continue
		}
		if strings.TrimSpace(release.Namespace) == "" {
			release.Namespace = item.Namespace
		}
		records = append(records, helmReleaseRecord{
			createdAt: item.CreationTimestamp.Time,
			labels:    cloneStringMap(item.Labels),
			release:   release,
			secret:    item.Name,
		})
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].release.Namespace != records[j].release.Namespace {
			return records[i].release.Namespace < records[j].release.Namespace
		}
		if records[i].release.Name != records[j].release.Name {
			return records[i].release.Name < records[j].release.Name
		}
		return records[i].release.Version > records[j].release.Version
	})
	return records, nil
}

func mapHelmReleaseDetail(record helmReleaseRecord) domainresource.HelmReleaseDetailView {
	release := record.release
	chartName := ""
	chartVersion := ""
	appVersion := ""
	description := ""
	annotations := map[string]string(nil)
	if release.Chart != nil && release.Chart.Metadata != nil {
		chartName = strings.TrimSpace(release.Chart.Metadata.Name)
		chartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		appVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		description = strings.TrimSpace(release.Chart.Metadata.Description)
		annotations = cloneStringMap(release.Chart.Metadata.Annotations)
	}
	status := strings.TrimSpace(record.labels["status"])
	if status == "" && release.Info != nil {
		status = strings.TrimSpace(release.Info.Status)
	}
	if status == "" {
		status = "unknown"
	}
	detail := domainresource.HelmReleaseDetailView{
		Name:              release.Name,
		Namespace:         release.Namespace,
		Revision:          strconv.Itoa(release.Version),
		Status:            status,
		Chart:             strings.TrimSpace(record.labels["helm.sh/chart"]),
		ChartName:         chartName,
		ChartVersion:      chartVersion,
		AppVersion:        appVersion,
		StorageDriver:     "secret",
		Description:       description,
		Labels:            cloneStringMap(record.labels),
		Annotations:       annotations,
		AgeSeconds:        secondsSince(record.createdAt),
		ValuesEditable:    false,
		ValuesDiffEnabled: true,
	}
	if detail.Chart == "" && chartName != "" {
		if chartVersion != "" {
			detail.Chart = fmt.Sprintf("%s-%s", chartName, chartVersion)
		} else {
			detail.Chart = chartName
		}
	}
	if release.Info != nil {
		detail.Status = firstNonEmptyHelm(detail.Status, strings.TrimSpace(release.Info.Status))
		detail.Notes = release.Info.Notes
		detail.CreatedAt = formatHelmTime(record.createdAt)
		detail.UpdatedAt = formatHelmTime(release.Info.LastDeployed)
		detail.FirstDeployedAt = formatHelmTime(release.Info.FirstDeployed)
		detail.LastDeployedAt = formatHelmTime(release.Info.LastDeployed)
		detail.Description = firstNonEmptyHelm(strings.TrimSpace(release.Info.Description), detail.Description)
	} else {
		detail.CreatedAt = formatHelmTime(record.createdAt)
	}
	return detail
}

func mapHelmReleaseHistory(record helmReleaseRecord) domainresource.HelmReleaseHistoryView {
	release := record.release
	item := domainresource.HelmReleaseHistoryView{
		Name:         release.Name,
		Namespace:    release.Namespace,
		Revision:     strconv.Itoa(release.Version),
		Status:       strings.TrimSpace(record.labels["status"]),
		Chart:        strings.TrimSpace(record.labels["helm.sh/chart"]),
		Description:  "",
		UpdatedAt:    "",
		CreatedAt:    formatHelmTime(record.createdAt),
		ValuesDigest: "",
	}
	if release.Chart != nil && release.Chart.Metadata != nil {
		item.ChartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		item.AppVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		if item.Chart == "" && release.Chart.Metadata.Name != "" {
			if item.ChartVersion != "" {
				item.Chart = fmt.Sprintf("%s-%s", release.Chart.Metadata.Name, item.ChartVersion)
			} else {
				item.Chart = release.Chart.Metadata.Name
			}
		}
	}
	if release.Info != nil {
		item.Status = firstNonEmptyHelm(item.Status, strings.TrimSpace(release.Info.Status))
		item.Description = strings.TrimSpace(release.Info.Description)
		item.UpdatedAt = formatHelmTime(release.Info.LastDeployed)
	}
	valuesContent, err := helmrelease.ValuesYAML(release)
	if err == nil {
		item.ValuesDigest = helmrelease.Digest(valuesContent)
	}
	item.ManifestDigest = helmrelease.Digest(release.Manifest)
	return item
}

func formatHelmTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func firstNonEmptyHelm(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
