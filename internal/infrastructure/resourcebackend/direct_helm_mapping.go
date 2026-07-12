package resourcebackend

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	helmrelease "github.com/opensoha/soha-contracts/helmrelease"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	helmreleasev1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

func mapSDKHelmReleaseDetail(release *helmreleasev1.Release) domainresource.HelmReleaseDetailView {
	if release == nil {
		return domainresource.HelmReleaseDetailView{}
	}
	chartName, chartVersion, appVersion, description := "", "", "", ""
	var annotations map[string]string
	if release.Chart != nil && release.Chart.Metadata != nil {
		chartName = strings.TrimSpace(release.Chart.Metadata.Name)
		chartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		appVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		description = strings.TrimSpace(release.Chart.Metadata.Description)
		annotations = cloneHelmStringMap(release.Chart.Metadata.Annotations)
	}
	detail := domainresource.HelmReleaseDetailView{
		Name: strings.TrimSpace(release.Name), Namespace: strings.TrimSpace(release.Namespace),
		Revision: strconv.Itoa(release.Version), ChartName: chartName, ChartVersion: chartVersion,
		AppVersion: appVersion, StorageDriver: "secret", Description: description,
		Annotations: annotations, ValuesEditable: false, ValuesDiffEnabled: true,
	}
	if chartName != "" {
		detail.Chart = chartName
		if chartVersion != "" {
			detail.Chart = fmt.Sprintf("%s-%s", chartName, chartVersion)
		}
	}
	if release.Info != nil {
		detail.Status = strings.TrimSpace(string(release.Info.Status))
		detail.Notes = release.Info.Notes
		detail.CreatedAt = formatHelmTime(release.Info.FirstDeployed)
		detail.UpdatedAt = formatHelmTime(release.Info.LastDeployed)
		detail.FirstDeployedAt = formatHelmTime(release.Info.FirstDeployed)
		detail.LastDeployedAt = formatHelmTime(release.Info.LastDeployed)
		detail.Description = firstNonEmptyHelm(strings.TrimSpace(release.Info.Description), detail.Description)
		if !release.Info.FirstDeployed.IsZero() {
			detail.AgeSeconds = secondsSince(release.Info.FirstDeployed)
		}
	}
	if detail.Status == "" {
		detail.Status = "unknown"
	}
	return detail
}

func mapSDKHelmReleaseHistory(release *helmreleasev1.Release) domainresource.HelmReleaseHistoryView {
	if release == nil {
		return domainresource.HelmReleaseHistoryView{}
	}
	item := domainresource.HelmReleaseHistoryView{
		Name: strings.TrimSpace(release.Name), Namespace: strings.TrimSpace(release.Namespace),
		Revision: strconv.Itoa(release.Version),
	}
	if release.Chart != nil && release.Chart.Metadata != nil {
		item.ChartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		item.AppVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		chartName := strings.TrimSpace(release.Chart.Metadata.Name)
		if chartName != "" {
			item.Chart = chartName
			if item.ChartVersion != "" {
				item.Chart = fmt.Sprintf("%s-%s", chartName, item.ChartVersion)
			}
		}
	}
	if release.Info != nil {
		item.Status = strings.TrimSpace(string(release.Info.Status))
		item.Description = strings.TrimSpace(release.Info.Description)
		item.UpdatedAt = formatHelmTime(release.Info.LastDeployed)
		item.CreatedAt = formatHelmTime(release.Info.FirstDeployed)
	}
	item.ManifestDigest = helmrelease.Digest(release.Manifest)
	if valuesContent, err := sdkReleaseValuesYAML(release); err == nil {
		item.ValuesDigest = helmrelease.Digest(valuesContent)
	}
	return item
}

func sdkReleaseValuesYAML(release *helmreleasev1.Release) (string, error) {
	if release == nil || len(release.Config) == 0 {
		return "{}\n", nil
	}
	content, err := yaml.Marshal(release.Config)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func mapHelmChartInstallResult(release *helmreleasev1.Release) domainresource.HelmChartInstallResult {
	if release == nil {
		return domainresource.HelmChartInstallResult{}
	}
	result := domainresource.HelmChartInstallResult{
		Name: strings.TrimSpace(release.Name), Namespace: strings.TrimSpace(release.Namespace),
		Revision: strconv.Itoa(release.Version),
	}
	if release.Info != nil {
		result.Status = strings.TrimSpace(string(release.Info.Status))
		result.Description = strings.TrimSpace(release.Info.Description)
		result.Notes = strings.TrimSpace(release.Info.Notes)
	}
	if release.Chart != nil && release.Chart.Metadata != nil {
		result.ChartName = strings.TrimSpace(release.Chart.Metadata.Name)
		result.ChartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		result.AppVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		result.Chart = result.ChartName
		if result.ChartName != "" && result.ChartVersion != "" {
			result.Chart = result.ChartName + "-" + result.ChartVersion
		}
	}
	result.Resources = mapHelmInstallManifestResources(release.Manifest)
	return result
}

func mapHelmInstallManifestResources(manifest string) []domainresource.HelmChartInstallResourceView {
	manifest = strings.TrimSpace(manifest)
	if manifest == "" {
		return nil
	}
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(manifest), 4096)
	resources := make([]domainresource.HelmChartInstallResourceView, 0)
	for len(resources) < maxHelmResourceSummaries {
		var object unstructured.Unstructured
		if err := decoder.Decode(&object); err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		if object.Object == nil {
			continue
		}
		name := strings.TrimSpace(object.GetName())
		kind := strings.TrimSpace(object.GetKind())
		if name == "" || kind == "" {
			continue
		}
		resources = append(resources, domainresource.HelmChartInstallResourceView{
			APIVersion: strings.TrimSpace(object.GetAPIVersion()), Kind: kind,
			Namespace: strings.TrimSpace(object.GetNamespace()), Name: name,
		})
	}
	return resources
}

func cloneHelmStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
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
