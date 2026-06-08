package resource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	helmreleasepkg "helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	defaultHelmInstallTimeoutSeconds = 300
	maxHelmInstallTimeoutSeconds     = 3600
	maxHelmInstallResourceSummaries  = 300
)

func (s *Service) InstallHelmChart(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	input = normalizeHelmChartInstallInput(input)
	if err := validateHelmChartInstallInput(input); err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, input.Namespace, "HelmRelease", domainaccess.ActionCreate)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		err := fmt.Errorf("%w: helm chart install is not supported for agent-connected clusters yet", apperrors.ErrInvalidArgument)
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", input.ReleaseName, string(domainaccess.ActionCreate), "failure", err.Error())
		return domainresource.HelmChartInstallResult{}, err
	}

	result, err := s.installDirectHelmChart(ctx, clusterID, input)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", input.ReleaseName, string(domainaccess.ActionCreate), "failure", err.Error())
		return domainresource.HelmChartInstallResult{}, err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", result.Name, string(domainaccess.ActionCreate), "success", fmt.Sprintf("installed helm chart %s %s", input.ChartName, input.Version))
	s.recordOperation(ctx, principal, "platform.helm_release.install", connection.Summary.ID, input.Namespace, "HelmRelease", result.Name, "installed helm chart", map[string]any{
		"repositoryName":  input.RepositoryName,
		"repositoryUrl":   input.RepositoryURL,
		"chartName":       input.ChartName,
		"version":         input.Version,
		"createNamespace": input.CreateNamespace,
		"wait":            input.Wait,
		"timeoutSeconds":  input.TimeoutSeconds,
	})
	return result, nil
}

func (s *Service) installDirectHelmChart(ctx context.Context, clusterID string, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	values, err := parseHelmInstallValues(input.ValuesYAML)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	settings, err := newSohaHelmEnvSettings(input.Namespace)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	actionConfig := new(action.Configuration)
	getter := helmRESTClientGetter{restConfig: bundle.RESTConfig, namespace: input.Namespace}
	if err := actionConfig.Init(getter, input.Namespace, "secrets", func(string, ...interface{}) {}); err != nil {
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: initialize helm action: %v", apperrors.ErrClusterUnready, err)
	}
	if existing, ok, err := existingDirectHelmInstallResultFromSDK(actionConfig, input); err != nil {
		return domainresource.HelmChartInstallResult{}, err
	} else if ok {
		return existing, nil
	}

	installer := action.NewInstall(actionConfig)
	installer.ReleaseName = input.ReleaseName
	installer.Namespace = input.Namespace
	installer.CreateNamespace = input.CreateNamespace
	installer.Wait = input.Wait
	installer.WaitForJobs = input.Wait
	installer.Timeout = time.Duration(input.TimeoutSeconds) * time.Second
	installer.DependencyUpdate = true
	installer.ChartPathOptions.RepoURL = input.RepositoryURL
	installer.ChartPathOptions.Version = input.Version

	chartPath, err := installer.ChartPathOptions.LocateChart(input.ChartName, settings)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: locate helm chart: %v", apperrors.ErrClusterUnready, err)
	}
	chart, err := loader.Load(chartPath)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: load helm chart: %v", apperrors.ErrClusterUnready, err)
	}
	release, err := installer.RunWithContext(ctx, chart, values)
	if err != nil {
		if isHelmReleaseNameInUseError(err) {
			if existing, ok, lookupErr := existingDirectHelmInstallResultFromSDK(actionConfig, input); lookupErr != nil {
				return domainresource.HelmChartInstallResult{}, lookupErr
			} else if ok {
				return existing, nil
			}
			return domainresource.HelmChartInstallResult{}, helmReleaseNameUnavailableError(input.ReleaseName, input.Namespace, helmReleaseRecord{})
		}
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: install helm chart: %v", apperrors.ErrClusterUnready, err)
	}
	return mapHelmChartInstallResult(release), nil
}

func existingDirectHelmInstallResultFromSDK(actionConfig *action.Configuration, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, bool, error) {
	release, err := action.NewGet(actionConfig).Run(input.ReleaseName)
	if err != nil {
		if isHelmReleaseNotFoundError(err) {
			return domainresource.HelmChartInstallResult{}, false, nil
		}
		return domainresource.HelmChartInstallResult{}, false, fmt.Errorf("%w: inspect existing helm release: %v", apperrors.ErrClusterUnready, err)
	}
	if helmSDKReleaseSatisfiesInstall(release, input) {
		result := mapHelmChartInstallResult(release)
		result.Description = firstNonEmptyHelm(result.Description, "Release already deployed; install request already satisfied")
		return result, true, nil
	}
	return domainresource.HelmChartInstallResult{}, false, helmReleaseNameUnavailableSDKError(input.ReleaseName, input.Namespace, release)
}

func helmReleaseNameUnavailableError(releaseName, namespace string, record helmReleaseRecord) error {
	status := helmReleaseRecordStatus(record)
	revision := ""
	if record.release != nil && record.release.Version > 0 {
		revision = strconv.Itoa(record.release.Version)
	}
	return helmReleaseNameUnavailableErrorParts(releaseName, namespace, status, revision)
}

func helmReleaseNameUnavailableSDKError(releaseName, namespace string, release *helmreleasepkg.Release) error {
	status := ""
	revision := ""
	if release != nil {
		if release.Info != nil {
			status = strings.TrimSpace(string(release.Info.Status))
		}
		if release.Version > 0 {
			revision = strconv.Itoa(release.Version)
		}
	}
	return helmReleaseNameUnavailableErrorParts(releaseName, namespace, status, revision)
}

func helmReleaseNameUnavailableErrorParts(releaseName, namespace, status, revision string) error {
	parts := []string{
		fmt.Sprintf("releaseName %q in namespace %q is already used by Helm release history", strings.TrimSpace(releaseName), strings.TrimSpace(namespace)),
	}
	if status != "" {
		parts = append(parts, fmt.Sprintf("status %q", status))
	}
	if revision != "" {
		parts = append(parts, fmt.Sprintf("revision %s", revision))
	}
	return fmt.Errorf("%w: %s; choose another release name or uninstall the existing release before installing again", apperrors.ErrInvalidArgument, strings.Join(parts, ", "))
}

func helmReleaseRecordStatus(record helmReleaseRecord) string {
	status := strings.TrimSpace(record.labels["status"])
	if status == "" && record.release != nil && record.release.Info != nil {
		status = strings.TrimSpace(record.release.Info.Status)
	}
	return status
}

func helmSDKReleaseSatisfiesInstall(release *helmreleasepkg.Release, input domainresource.HelmChartInstallInput) bool {
	if release == nil || release.Chart == nil || release.Chart.Metadata == nil {
		return false
	}
	status := ""
	if release.Info != nil {
		status = strings.TrimSpace(string(release.Info.Status))
	}
	if !strings.EqualFold(status, "deployed") {
		return false
	}
	metadata := release.Chart.Metadata
	chartName := strings.TrimSpace(metadata.Name)
	chartVersion := strings.TrimSpace(metadata.Version)
	return strings.EqualFold(chartName, strings.TrimSpace(input.ChartName)) && chartVersion == strings.TrimSpace(input.Version)
}

func isHelmReleaseNameInUseError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "cannot re-use a name that is still in use")
}

func normalizeHelmChartInstallInput(input domainresource.HelmChartInstallInput) domainresource.HelmChartInstallInput {
	input.RepositoryName = strings.TrimSpace(input.RepositoryName)
	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	input.ChartName = strings.TrimSpace(input.ChartName)
	input.Version = strings.TrimSpace(input.Version)
	input.ReleaseName = strings.TrimSpace(input.ReleaseName)
	input.Namespace = strings.TrimSpace(input.Namespace)
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = defaultHelmInstallTimeoutSeconds
	}
	if input.TimeoutSeconds > maxHelmInstallTimeoutSeconds {
		input.TimeoutSeconds = maxHelmInstallTimeoutSeconds
	}
	return input
}

func validateHelmChartInstallInput(input domainresource.HelmChartInstallInput) error {
	if input.RepositoryURL == "" {
		return fmt.Errorf("%w: repositoryUrl is required", apperrors.ErrInvalidArgument)
	}
	if input.ChartName == "" {
		return fmt.Errorf("%w: chartName is required", apperrors.ErrInvalidArgument)
	}
	if input.Version == "" {
		return fmt.Errorf("%w: version is required", apperrors.ErrInvalidArgument)
	}
	if input.ReleaseName == "" {
		return fmt.Errorf("%w: releaseName is required", apperrors.ErrInvalidArgument)
	}
	if input.Namespace == "" {
		return fmt.Errorf("%w: namespace is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func parseHelmInstallValues(valuesYAML string) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	if strings.TrimSpace(valuesYAML) == "" {
		return values, nil
	}
	if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil {
		return nil, fmt.Errorf("%w: invalid values yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	if values == nil {
		return map[string]interface{}{}, nil
	}
	return values, nil
}

func newSohaHelmEnvSettings(namespace string) (*cli.EnvSettings, error) {
	root := filepath.Join(os.TempDir(), "soha-helm")
	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: prepare helm cache: %v", apperrors.ErrClusterUnready, err)
	}
	settings := cli.New()
	settings.SetNamespace(namespace)
	settings.RepositoryCache = cacheDir
	settings.RepositoryConfig = filepath.Join(root, "repositories.yaml")
	settings.RegistryConfig = filepath.Join(root, "registry.json")
	settings.PluginsDirectory = filepath.Join(root, "plugins")
	return settings, nil
}

func mapHelmChartInstallResult(release *helmreleasepkg.Release) domainresource.HelmChartInstallResult {
	if release == nil {
		return domainresource.HelmChartInstallResult{}
	}
	result := domainresource.HelmChartInstallResult{
		Name:      strings.TrimSpace(release.Name),
		Namespace: strings.TrimSpace(release.Namespace),
		Revision:  strconv.Itoa(release.Version),
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
		if result.ChartName != "" && result.ChartVersion != "" {
			result.Chart = result.ChartName + "-" + result.ChartVersion
		} else {
			result.Chart = result.ChartName
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
	for len(resources) < maxHelmInstallResourceSummaries {
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
			APIVersion: strings.TrimSpace(object.GetAPIVersion()),
			Kind:       kind,
			Namespace:  strings.TrimSpace(object.GetNamespace()),
			Name:       name,
		})
	}
	return resources
}

type helmRESTClientGetter struct {
	restConfig *rest.Config
	namespace  string
}

func (g helmRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	if g.restConfig == nil {
		return nil, fmt.Errorf("%w: missing kubernetes rest config", apperrors.ErrClusterUnready)
	}
	return rest.CopyConfig(g.restConfig), nil
}

func (g helmRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	config, err := g.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(client), nil
}

func (g helmRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, err
	}
	return restmapper.NewDiscoveryRESTMapper(groupResources), nil
}

func (g helmRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	namespace := strings.TrimSpace(g.namespace)
	if namespace == "" {
		namespace = "default"
	}
	rawConfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster": {Server: ""},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"user": {},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"context": {Cluster: "cluster", AuthInfo: "user", Namespace: namespace},
		},
		CurrentContext: "context",
	}
	return clientcmd.NewDefaultClientConfig(rawConfig, &clientcmd.ConfigOverrides{
		Context: clientcmdapi.Context{Namespace: namespace},
	})
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
