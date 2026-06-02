package resource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	helmreleasepkg "helm.sh/helm/v3/pkg/release"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestMapArtifactHubChartMapsPackageMetadata(t *testing.T) {
	t.Parallel()

	chart := mapArtifactHubChart(artifactHubPackage{
		PackageID:       "pkg-1",
		Name:            "openebs",
		NormalizedName:  "openebs",
		Description:     "Containerized storage",
		Version:         "4.5.0",
		AppVersion:      "4.5.0",
		LogoImageID:     "logo-1",
		Stars:           42,
		Official:        true,
		Signed:          true,
		HasValuesSchema: true,
		TS:              1780075359,
		Keywords:        []string{" storage ", "kubernetes"},
		Repository: artifactHubRepository{
			URL:               "https://openebs.github.io/openebs",
			Name:              "openebs",
			DisplayName:       "OpenEBS",
			VerifiedPublisher: true,
		},
		SecurityReportSummary: artifactHubSecuritySummary{Critical: 1, High: 2, Medium: 3, Low: 4},
	})

	if chart.PackageID != "pkg-1" {
		t.Fatalf("PackageID = %q, want pkg-1", chart.PackageID)
	}
	if chart.RepositoryName != "openebs" || chart.RepositoryDisplay != "OpenEBS" {
		t.Fatalf("repository fields = %#v, want openebs/OpenEBS", chart)
	}
	if chart.LatestVersion != "4.5.0" || chart.VersionCount != 1 {
		t.Fatalf("version fields = %#v, want latest version and one version", chart)
	}
	if chart.ArtifactHubURL != "https://artifacthub.io/packages/helm/openebs/openebs" {
		t.Fatalf("ArtifactHubURL = %q", chart.ArtifactHubURL)
	}
	if chart.LogoImageURL != "https://artifacthub.io/image/logo-1" {
		t.Fatalf("LogoImageURL = %q", chart.LogoImageURL)
	}
	if !chart.Official || !chart.Signed || !chart.HasValuesSchema || !chart.VerifiedPublisher {
		t.Fatalf("expected package flags to be mapped: %#v", chart)
	}
	if chart.Keywords[0] != "storage" {
		t.Fatalf("Keywords = %#v, want trimmed storage", chart.Keywords)
	}
	if chart.SecurityCritical != 1 || chart.SecurityHigh != 2 {
		t.Fatalf("security summary = %#v", chart)
	}
}

func TestFetchArtifactHubHelmCatalogMapsPaginationTotal(t *testing.T) {
	t.Parallel()

	service := &Service{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Query().Get("kind") != artifactHubHelmKind {
					t.Fatalf("kind query = %q, want %q", request.URL.Query().Get("kind"), artifactHubHelmKind)
				}
				if request.URL.Query().Get("limit") != "60" {
					t.Fatalf("limit query = %q, want 60", request.URL.Query().Get("limit"))
				}
				if request.URL.Query().Get("offset") != "120" {
					t.Fatalf("offset query = %q, want 120", request.URL.Query().Get("offset"))
				}
				if request.URL.Query().Get("ts_query_web") != "prometheus" {
					t.Fatalf("ts_query_web query = %q, want prometheus", request.URL.Query().Get("ts_query_web"))
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header: http.Header{
						"Pagination-Total-Count": []string{"17043"},
					},
					Body: io.NopCloser(strings.NewReader(`{
						"packages": [
							{
								"package_id": "pkg-prometheus",
								"name": "prometheus",
								"version": "15.0.0",
								"repository": {
									"name": "prometheus-community",
									"url": "https://prometheus-community.github.io/helm-charts"
								}
							}
						]
					}`)),
				}, nil
			}),
		},
	}

	catalog, err := service.fetchArtifactHubHelmCatalog(context.Background(), " prometheus ", 100, 120)
	if err != nil {
		t.Fatalf("fetchArtifactHubHelmCatalog returned error: %v", err)
	}
	if catalog.TotalCount != 17043 {
		t.Fatalf("TotalCount = %d, want 17043", catalog.TotalCount)
	}
	if catalog.ChartCount != 1 || catalog.LoadedCount != 1 {
		t.Fatalf("loaded counts = chartCount %d loadedCount %d, want 1/1", catalog.ChartCount, catalog.LoadedCount)
	}
	if catalog.Limit != 60 || catalog.Offset != 120 || catalog.Query != "prometheus" {
		t.Fatalf("catalog pagination/query = %#v", catalog)
	}
}

func TestParseHelmInstallValuesValidatesYAML(t *testing.T) {
	t.Parallel()

	values, err := parseHelmInstallValues("replicaCount: 2\nimage:\n  tag: latest\n")
	if err != nil {
		t.Fatalf("parseHelmInstallValues returned error: %v", err)
	}
	if values["replicaCount"] != float64(2) {
		t.Fatalf("replicaCount = %#v, want 2", values["replicaCount"])
	}
	image, ok := values["image"].(map[string]interface{})
	if !ok || image["tag"] != "latest" {
		t.Fatalf("image values = %#v", values["image"])
	}

	if _, err := parseHelmInstallValues("replicaCount: ["); err == nil {
		t.Fatalf("parseHelmInstallValues invalid YAML returned nil error")
	}
}

func TestMapHelmChartInstallResultIncludesManifestResources(t *testing.T) {
	t.Parallel()

	result := mapHelmChartInstallResult(&helmreleasepkg.Release{
		Name:      "prometheus",
		Namespace: "monitoring",
		Version:   1,
		Manifest: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus-operator
  namespace: monitoring
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: prometheus-operator
  namespace: monitoring
`,
	})

	if len(result.Resources) != 2 {
		t.Fatalf("Resources length = %d, want 2", len(result.Resources))
	}
	if result.Resources[0].Kind != "Deployment" || result.Resources[0].Name != "prometheus-operator" || result.Resources[0].Namespace != "monitoring" {
		t.Fatalf("first resource = %#v", result.Resources[0])
	}
	if result.Resources[1].Kind != "ServiceAccount" || result.Resources[1].APIVersion != "v1" {
		t.Fatalf("second resource = %#v", result.Resources[1])
	}
}
