package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	artifactHubAPIBaseURL         = "https://artifacthub.io/api/v1"
	artifactHubWebBaseURL         = "https://artifacthub.io"
	artifactHubHelmKind           = "0"
	defaultArtifactHubSearchLimit = 60
	maxArtifactHubSearchLimit     = 60
	artifactHubJSONMaxBytes       = 4 * 1024 * 1024
	artifactHubValuesMaxBytes     = 3 * 1024 * 1024
)

type artifactHubSearchResponse struct {
	Packages []artifactHubPackage `json:"packages"`
}

type artifactHubPackage struct {
	PackageID             string                     `json:"package_id"`
	Name                  string                     `json:"name"`
	NormalizedName        string                     `json:"normalized_name"`
	Category              any                        `json:"category"`
	LogoImageID           string                     `json:"logo_image_id"`
	Stars                 int                        `json:"stars"`
	Official              bool                       `json:"official"`
	CNCF                  bool                       `json:"cncf"`
	Description           string                     `json:"description"`
	Version               string                     `json:"version"`
	AppVersion            string                     `json:"app_version"`
	Deprecated            bool                       `json:"deprecated"`
	HasValuesSchema       bool                       `json:"has_values_schema"`
	Signed                bool                       `json:"signed"`
	TS                    int64                      `json:"ts"`
	Repository            artifactHubRepository      `json:"repository"`
	HomeURL               string                     `json:"home_url"`
	Readme                string                     `json:"readme"`
	Keywords              []string                   `json:"keywords"`
	ContentURL            string                     `json:"content_url"`
	Digest                string                     `json:"digest"`
	Links                 []artifactHubLink          `json:"links"`
	AvailableVersions     []artifactHubVersion       `json:"available_versions"`
	Maintainers           []artifactHubMaintainer    `json:"maintainers"`
	SecurityReportSummary artifactHubSecuritySummary `json:"security_report_summary"`
}

type artifactHubRepository struct {
	URL                     string `json:"url"`
	Name                    string `json:"name"`
	DisplayName             string `json:"display_name"`
	RepositoryID            string `json:"repository_id"`
	OrganizationName        string `json:"organization_name"`
	OrganizationDisplayName string `json:"organization_display_name"`
	Official                bool   `json:"official"`
	CNCF                    bool   `json:"cncf"`
	VerifiedPublisher       bool   `json:"verified_publisher"`
}

type artifactHubVersion struct {
	Version                 string `json:"version"`
	AppVersion              string `json:"app_version"`
	TS                      int64  `json:"ts"`
	Prerelease              bool   `json:"prerelease"`
	ContainsSecurityUpdates bool   `json:"contains_security_updates"`
}

type artifactHubLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type artifactHubMaintainer struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	URL   string `json:"url"`
}

type artifactHubSecuritySummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

func (h *Helm) ListHelmCharts(ctx context.Context, principal domainidentity.Principal, clusterID, keyword string, limit, offset int) (domainresource.HelmChartCatalogView, error) {
	s := h
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "HelmChart", domainaccess.ActionList)
	if err != nil {
		return domainresource.HelmChartCatalogView{}, err
	}
	item, err := s.fetchArtifactHubHelmCatalog(ctx, keyword, limit, offset)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "HelmChart", "", string(domainaccess.ActionList), "failure", err.Error())
		return domainresource.HelmChartCatalogView{}, err
	}
	populateAllowedActionsHelmCharts(item.Charts, s.helmChartInstallAllowedActions(ctx, principal, connection, ""))
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "HelmChart", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed helm charts from Artifact Hub query %q", item.Query))
	return item, nil
}

func (h *Helm) GetHelmChartDetail(ctx context.Context, principal domainidentity.Principal, clusterID, repositoryName, chartName, version string) (domainresource.HelmChartDetailView, error) {
	s := h
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "HelmChart", domainaccess.ActionView)
	if err != nil {
		return domainresource.HelmChartDetailView{}, err
	}
	item, err := s.fetchArtifactHubHelmChartDetail(ctx, repositoryName, chartName, version)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "HelmChart", chartName, string(domainaccess.ActionView), "failure", err.Error())
		return domainresource.HelmChartDetailView{}, err
	}
	item.AllowedActions = s.helmChartInstallAllowedActions(ctx, principal, connection, "")
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "HelmChart", chartName, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed Artifact Hub package %s/%s", repositoryName, chartName))
	return item, nil
}

func (h *Helm) GetHelmChartValuesTemplate(ctx context.Context, principal domainidentity.Principal, clusterID, packageID, name, version string) (domainresource.HelmChartValuesTemplateView, error) {
	s := h
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "HelmChart", domainaccess.ActionView)
	if err != nil {
		return domainresource.HelmChartValuesTemplateView{}, err
	}
	item, err := s.fetchArtifactHubHelmChartValues(ctx, packageID, name, version)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "HelmChart", name, string(domainaccess.ActionView), "failure", err.Error())
		return domainresource.HelmChartValuesTemplateView{}, err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "HelmChart", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed Artifact Hub values for %s", packageID))
	return item, nil
}

func (h *Helm) GetHelmReleaseDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.HelmReleaseDetailView, error) {
	s := h
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
		client, err := s.helmAgentClient(connection)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, err
		}
		item, err = client.GetHelmReleaseDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.direct.GetHelmReleaseDetail(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.HelmReleaseDetailView{}, err
		}
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	item.ValuesEditable = helmReleaseValuesEditable(connection, decision)
	item.ValuesDiffEnabled = true
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed helm release detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (h *Helm) ListHelmReleaseHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.HelmReleaseHistoryView, error) {
	s := h
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
		client, err := s.helmAgentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHelmReleaseHistory(ctx, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.direct.ListHelmReleaseHistory(ctx, clusterID, namespace, name)
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

func (h *Helm) GetHelmReleaseValues(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revision string) (domainresource.HelmValuesView, error) {
	s := h
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
		client, err := s.helmAgentClient(connection)
		if err != nil {
			return domainresource.HelmValuesView{}, err
		}
		item, err = client.GetHelmReleaseValues(ctx, namespace, name, revision)
		if err != nil {
			return domainresource.HelmValuesView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.direct.GetHelmReleaseValues(ctx, clusterID, namespace, name, revision)
		if err != nil {
			return domainresource.HelmValuesView{}, err
		}
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	item.Editable = helmReleaseValuesEditable(connection, decision)
	item.DiffEnabled = true
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed helm release values via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (h *Helm) UpdateHelmReleaseValues(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.HelmValuesView, error) {
	s := h
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" {
		return domainresource.HelmValuesView{}, fmt.Errorf("%w: namespace and releaseName are required", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		normalizedContent := normalizeHelmValuesContent(content)
		if _, err := parseHelmInstallValues(normalizedContent); err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.HelmValuesView{}, err
		}
		client, err := s.helmAgentClient(connection)
		if err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.HelmValuesView{}, err
		}
		item, err := client.UpdateHelmReleaseValues(ctx, namespace, name, normalizedContent)
		if err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.HelmValuesView{}, err
		}
		item.AllowedActions = stringifyActions(decision.AllowedActions)
		item.Editable = helmReleaseValuesEditable(connection, decision)
		item.DiffEnabled = true
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionUpdate), "success", "updated helm release values via agent")
		s.recordOperation(ctx, principal, "platform.helm_release.values_update", connection.Summary.ID, namespace, "HelmRelease", name, "updated helm release values via agent", map[string]any{
			"revision": item.Revision,
			"source":   "agent",
		})
		return item, nil
	}

	normalizedContent := normalizeHelmValuesContent(content)
	item, err := s.direct.UpdateHelmReleaseValues(ctx, clusterID, namespace, name, normalizedContent)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.HelmValuesView{}, err
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	item.Editable = helmReleaseValuesEditable(connection, decision)
	item.DiffEnabled = true
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionUpdate), "success", "updated helm release values")
	s.recordOperation(ctx, principal, "platform.helm_release.values_update", connection.Summary.ID, namespace, "HelmRelease", name, "updated helm release values", map[string]any{
		"revision": item.Revision,
	})
	return item, nil
}

func (h *Helm) DeleteHelmRelease(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	s := h
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" {
		return fmt.Errorf("%w: namespace and releaseName are required", apperrors.ErrInvalidArgument)
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := s.helmAgentClient(connection)
		if err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		if err := client.DeleteHelmRelease(ctx, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionDelete), "success", "deleted helm release via agent")
		s.recordOperation(ctx, principal, "platform.helm_release.delete", connection.Summary.ID, namespace, "HelmRelease", name, "deleted helm release via agent", map[string]any{"source": "agent"})
		return nil
	}
	if err := s.direct.DeleteHelmRelease(ctx, clusterID, namespace, name); err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionDelete), "failure", err.Error())
		return err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HelmRelease", name, string(domainaccess.ActionDelete), "success", "deleted helm release")
	s.recordOperation(ctx, principal, "platform.helm_release.delete", connection.Summary.ID, namespace, "HelmRelease", name, "deleted helm release", nil)
	return nil
}

func (s *Helm) fetchArtifactHubHelmCatalog(ctx context.Context, keyword string, limit, offset int) (domainresource.HelmChartCatalogView, error) {
	normalizedLimit := normalizeHelmChartSearchLimit(limit)
	normalizedOffset := maxInt(offset, 0)
	endpoint, err := url.Parse(artifactHubAPIBaseURL + "/packages/search")
	if err != nil {
		return domainresource.HelmChartCatalogView{}, err
	}
	query := endpoint.Query()
	query.Set("kind", artifactHubHelmKind)
	query.Set("limit", strconv.Itoa(normalizedLimit))
	query.Set("offset", strconv.Itoa(normalizedOffset))
	normalizedKeyword := strings.TrimSpace(keyword)
	if normalizedKeyword != "" {
		query.Set("ts_query_web", normalizedKeyword)
	}
	endpoint.RawQuery = query.Encode()

	var payload artifactHubSearchResponse
	headers, err := s.fetchArtifactHubJSONWithHeaders(ctx, endpoint.String(), &payload)
	if err != nil {
		return domainresource.HelmChartCatalogView{}, err
	}
	charts := make([]domainresource.HelmChartView, 0, len(payload.Packages))
	for _, item := range payload.Packages {
		view := mapArtifactHubChart(item)
		if view.Name == "" {
			continue
		}
		charts = append(charts, view)
	}
	versionCount := 0
	for _, chart := range charts {
		if chart.VersionCount > 0 {
			versionCount += chart.VersionCount
			continue
		}
		if chart.LatestVersion != "" {
			versionCount++
		}
	}
	totalCount := artifactHubPaginationTotalCount(headers.Get("pagination-total-count"))
	if minimumTotal := normalizedOffset + len(charts); totalCount < minimumTotal {
		totalCount = minimumTotal
	}
	return domainresource.HelmChartCatalogView{
		Repository:   artifactHubRepositoryView(),
		Source:       "artifacthub",
		Query:        normalizedKeyword,
		Limit:        normalizedLimit,
		Offset:       normalizedOffset,
		RefreshedAt:  time.Now().UTC().Format(time.RFC3339),
		TotalCount:   totalCount,
		LoadedCount:  len(charts),
		ChartCount:   len(charts),
		VersionCount: versionCount,
		Charts:       charts,
	}, nil
}

func (s *Helm) fetchArtifactHubHelmChartDetail(ctx context.Context, repositoryName, chartName, version string) (domainresource.HelmChartDetailView, error) {
	repositoryName = strings.TrimSpace(repositoryName)
	chartName = strings.TrimSpace(chartName)
	version = strings.TrimSpace(version)
	if repositoryName == "" || chartName == "" {
		return domainresource.HelmChartDetailView{}, fmt.Errorf("%w: repositoryName and chartName are required", apperrors.ErrInvalidArgument)
	}
	endpoint := artifactHubAPIBaseURL + "/packages/helm/" + url.PathEscape(repositoryName) + "/" + url.PathEscape(chartName)
	if version != "" {
		endpoint += "/" + url.PathEscape(version)
	}
	var payload artifactHubPackage
	if err := s.fetchArtifactHubJSON(ctx, endpoint, &payload); err != nil {
		return domainresource.HelmChartDetailView{}, err
	}
	view := domainresource.HelmChartDetailView{
		HelmChartView:     mapArtifactHubChart(payload),
		Readme:            strings.TrimSpace(payload.Readme),
		ContentURL:        strings.TrimSpace(payload.ContentURL),
		Links:             mapArtifactHubLinks(payload.Links),
		AvailableVersions: mapArtifactHubVersions(payload.AvailableVersions),
	}
	if view.VersionCount == 0 {
		view.VersionCount = len(view.AvailableVersions)
	}
	if len(view.Versions) == 0 && len(view.AvailableVersions) > 0 {
		view.Versions = make([]string, 0, len(view.AvailableVersions))
		for _, item := range view.AvailableVersions {
			if item.Version != "" {
				view.Versions = append(view.Versions, item.Version)
			}
		}
	}
	return view, nil
}

func (s *Helm) fetchArtifactHubHelmChartValues(ctx context.Context, packageID, name, version string) (domainresource.HelmChartValuesTemplateView, error) {
	packageID = strings.TrimSpace(packageID)
	version = strings.TrimSpace(version)
	if packageID == "" || version == "" {
		return domainresource.HelmChartValuesTemplateView{}, fmt.Errorf("%w: packageId and version are required", apperrors.ErrInvalidArgument)
	}
	endpoint := artifactHubAPIBaseURL + "/packages/" + url.PathEscape(packageID) + "/" + url.PathEscape(version) + "/values"
	content, err := s.fetchArtifactHubText(ctx, endpoint, artifactHubValuesMaxBytes, "application/yaml, text/yaml, text/plain, */*")
	if err != nil {
		return domainresource.HelmChartValuesTemplateView{}, err
	}
	return domainresource.HelmChartValuesTemplateView{
		PackageID: packageID,
		Name:      strings.TrimSpace(name),
		Version:   version,
		Content:   content,
	}, nil
}

func (s *Helm) fetchArtifactHubJSON(ctx context.Context, endpoint string, target any) error {
	_, err := s.fetchArtifactHubJSONWithHeaders(ctx, endpoint, target)
	return err
}

func (s *Helm) fetchArtifactHubJSONWithHeaders(ctx context.Context, endpoint string, target any) (http.Header, error) {
	raw, headers, err := s.fetchArtifactHubBytesWithHeaders(ctx, endpoint, artifactHubJSONMaxBytes, "application/json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, fmt.Errorf("%w: parse Artifact Hub response: %v", apperrors.ErrClusterUnready, err)
	}
	return headers, nil
}

func (s *Helm) fetchArtifactHubText(ctx context.Context, endpoint string, maxBytes int64, accept string) (string, error) {
	raw, err := s.fetchArtifactHubBytes(ctx, endpoint, maxBytes, accept)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Helm) fetchArtifactHubBytes(ctx context.Context, endpoint string, maxBytes int64, accept string) ([]byte, error) {
	raw, _, err := s.fetchArtifactHubBytesWithHeaders(ctx, endpoint, maxBytes, accept)
	return raw, err
}

func (s *Helm) fetchArtifactHubBytesWithHeaders(ctx context.Context, endpoint string, maxBytes int64, accept string) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", accept)
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: fetch Artifact Hub: %v", apperrors.ErrClusterUnready, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, fmt.Errorf("%w: Artifact Hub returned %s", apperrors.ErrClusterUnready, response.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read Artifact Hub response: %v", apperrors.ErrClusterUnready, err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, nil, fmt.Errorf("%w: Artifact Hub response exceeds %d bytes", apperrors.ErrClusterUnready, maxBytes)
	}
	return raw, response.Header.Clone(), nil
}

func artifactHubRepositoryView() domainresource.HelmChartRepositoryView {
	return domainresource.HelmChartRepositoryView{
		ID:          "artifacthub",
		Name:        "Artifact Hub",
		DisplayName: "Artifact Hub",
		URL:         artifactHubWebBaseURL,
	}
}

func mapArtifactHubChart(item artifactHubPackage) domainresource.HelmChartView {
	repository := item.Repository
	name := strings.TrimSpace(item.Name)
	repositoryName := strings.TrimSpace(repository.Name)
	version := strings.TrimSpace(item.Version)
	chart := domainresource.HelmChartView{
		PackageID:         strings.TrimSpace(item.PackageID),
		Name:              name,
		NormalizedName:    strings.TrimSpace(item.NormalizedName),
		RepositoryName:    repositoryName,
		RepositoryURL:     strings.TrimSpace(repository.URL),
		RepositoryDisplay: firstNonEmptyHelm(strings.TrimSpace(repository.DisplayName), repositoryName),
		LatestVersion:     version,
		AppVersion:        strings.TrimSpace(item.AppVersion),
		Description:       strings.TrimSpace(item.Description),
		Category:          artifactHubCategoryString(item.Category),
		Deprecated:        item.Deprecated,
		Home:              strings.TrimSpace(item.HomeURL),
		HomeURL:           strings.TrimSpace(item.HomeURL),
		LogoImageID:       strings.TrimSpace(item.LogoImageID),
		LogoImageURL:      artifactHubLogoURL(item.LogoImageID),
		ArtifactHubURL:    artifactHubPackageWebURL(repositoryName, name),
		UpdatedAt:         artifactHubUnixTime(item.TS),
		Digest:            strings.TrimSpace(item.Digest),
		Keywords:          trimStringSlice(item.Keywords),
		Maintainers:       mapArtifactHubMaintainers(item.Maintainers),
		VersionCount:      len(item.AvailableVersions),
		Stars:             item.Stars,
		Official:          item.Official || repository.Official,
		CNCF:              item.CNCF || repository.CNCF,
		Signed:            item.Signed,
		HasValuesSchema:   item.HasValuesSchema,
		VerifiedPublisher: repository.VerifiedPublisher,
		SecurityCritical:  item.SecurityReportSummary.Critical,
		SecurityHigh:      item.SecurityReportSummary.High,
		SecurityMedium:    item.SecurityReportSummary.Medium,
		SecurityLow:       item.SecurityReportSummary.Low,
		SecurityUnknown:   item.SecurityReportSummary.Unknown,
	}
	if chart.VersionCount == 0 && version != "" {
		chart.VersionCount = 1
	}
	if version != "" {
		chart.Versions = []string{version}
	}
	return chart
}

func mapArtifactHubVersions(items []artifactHubVersion) []domainresource.HelmChartVersionView {
	if len(items) == 0 {
		return nil
	}
	result := make([]domainresource.HelmChartVersionView, 0, len(items))
	for _, item := range items {
		version := strings.TrimSpace(item.Version)
		if version == "" {
			continue
		}
		result = append(result, domainresource.HelmChartVersionView{
			Version:                 version,
			AppVersion:              strings.TrimSpace(item.AppVersion),
			CreatedAt:               artifactHubUnixTime(item.TS),
			Prerelease:              item.Prerelease,
			ContainsSecurityUpdates: item.ContainsSecurityUpdates,
		})
	}
	return result
}

func mapArtifactHubLinks(items []artifactHubLink) []domainresource.HelmChartLinkView {
	if len(items) == 0 {
		return nil
	}
	result := make([]domainresource.HelmChartLinkView, 0, len(items))
	for _, item := range items {
		view := domainresource.HelmChartLinkView{Name: strings.TrimSpace(item.Name), URL: strings.TrimSpace(item.URL)}
		if view.Name == "" && view.URL == "" {
			continue
		}
		result = append(result, view)
	}
	return result
}

func mapArtifactHubMaintainers(items []artifactHubMaintainer) []domainresource.HelmChartMaintainerView {
	if len(items) == 0 {
		return nil
	}
	result := make([]domainresource.HelmChartMaintainerView, 0, len(items))
	for _, item := range items {
		view := domainresource.HelmChartMaintainerView{
			Name:  strings.TrimSpace(item.Name),
			Email: strings.TrimSpace(item.Email),
			URL:   strings.TrimSpace(item.URL),
		}
		if view.Name == "" && view.Email == "" && view.URL == "" {
			continue
		}
		result = append(result, view)
	}
	return result
}

func normalizeHelmChartSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultArtifactHubSearchLimit
	}
	if limit > maxArtifactHubSearchLimit {
		return maxArtifactHubSearchLimit
	}
	return limit
}

func artifactHubPaginationTotalCount(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	total, err := strconv.Atoi(value)
	if err != nil || total < 0 {
		return 0
	}
	return total
}

func artifactHubPackageWebURL(repositoryName, chartName string) string {
	repositoryName = strings.TrimSpace(repositoryName)
	chartName = strings.TrimSpace(chartName)
	if repositoryName == "" || chartName == "" {
		return ""
	}
	return artifactHubWebBaseURL + "/packages/helm/" + url.PathEscape(repositoryName) + "/" + url.PathEscape(chartName)
}

func artifactHubLogoURL(logoImageID string) string {
	logoImageID = strings.TrimSpace(logoImageID)
	if logoImageID == "" {
		return ""
	}
	return artifactHubWebBaseURL + "/image/" + url.PathEscape(logoImageID)
}

func artifactHubUnixTime(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func artifactHubCategoryString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == 0 {
			return ""
		}
		return strconv.Itoa(int(typed))
	case int:
		if typed == 0 {
			return ""
		}
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func trimStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func (s *Helm) helmChartInstallAllowedActions(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace string) []string {
	return s.allowedActionsForResource(ctx, principal, connection, namespace, "HelmRelease", domainaccess.ActionCreate)
}

func populateAllowedActionsHelmCharts(items []domainresource.HelmChartView, actions []string) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = actions
		}
	}
}

func helmReleaseValuesEditable(connection domaincluster.Connection, decision domainaccess.Decision) bool {
	return decisionAllowsAction(decision, domainaccess.ActionUpdate)
}

func decisionAllowsAction(decision domainaccess.Decision, action domainaccess.Action) bool {
	for _, allowedAction := range decision.AllowedActions {
		if allowedAction == action {
			return true
		}
	}
	return false
}

func normalizeHelmValuesContent(content string) string {
	if strings.TrimSpace(content) == "" {
		return "{}\n"
	}
	return content
}

func firstNonEmptyHelm(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
