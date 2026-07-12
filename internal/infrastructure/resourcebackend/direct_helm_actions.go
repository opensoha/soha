package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	helmreleasepkg "helm.sh/helm/v4/pkg/release"
	helmreleasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

const (
	directHelmTimeoutSeconds = 300
	maxHelmResourceSummaries = 300
)

func (d *Direct) GetHelmReleaseDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.HelmReleaseDetailView, error) {
	actionConfig, err := d.helmActionConfig(ctx, clusterID, namespace)
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}
	release, err := action.NewGet(actionConfig).Run(name)
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, mapHelmReleaseSDKError(name, "get helm release detail", err)
	}
	releaseV1, err := helmSDKReleaseV1(release)
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, mapHelmReleaseSDKError(name, "read helm release detail", err)
	}
	return mapSDKHelmReleaseDetail(releaseV1), nil
}

func (d *Direct) ListHelmReleaseHistory(ctx context.Context, clusterID, namespace, name string) ([]domainresource.HelmReleaseHistoryView, error) {
	actionConfig, err := d.helmActionConfig(ctx, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	releases, err := action.NewHistory(actionConfig).Run(name)
	if err != nil {
		return nil, mapHelmReleaseSDKError(name, "list helm release history", err)
	}
	items := make([]domainresource.HelmReleaseHistoryView, 0, len(releases))
	for _, release := range releases {
		releaseV1, err := helmSDKReleaseV1(release)
		if err != nil {
			return nil, mapHelmReleaseSDKError(name, "read helm release history", err)
		}
		items = append(items, mapSDKHelmReleaseHistory(releaseV1))
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftRevision, _ := strconv.Atoi(items[i].Revision)
		rightRevision, _ := strconv.Atoi(items[j].Revision)
		return leftRevision > rightRevision
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: helm release %s not found", apperrors.ErrNotFound, name)
	}
	return items, nil
}

func (d *Direct) GetHelmReleaseValues(ctx context.Context, clusterID, namespace, name, revision string) (domainresource.HelmValuesView, error) {
	actionConfig, err := d.helmActionConfig(ctx, clusterID, namespace)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	getter := action.NewGet(actionConfig)
	if strings.TrimSpace(revision) != "" {
		parsedRevision, err := strconv.Atoi(strings.TrimSpace(revision))
		if err != nil || parsedRevision <= 0 {
			return domainresource.HelmValuesView{}, fmt.Errorf("%w: revision must be a positive integer", apperrors.ErrInvalidArgument)
		}
		getter.Version = parsedRevision
	}
	release, err := getter.Run(name)
	if err != nil {
		return domainresource.HelmValuesView{}, mapHelmReleaseSDKError(name, "get helm release values", err)
	}
	releaseV1, err := helmSDKReleaseV1(release)
	if err != nil {
		return domainresource.HelmValuesView{}, mapHelmReleaseSDKError(name, "read helm release values", err)
	}
	content, err := sdkReleaseValuesYAML(releaseV1)
	if err != nil {
		return domainresource.HelmValuesView{}, fmt.Errorf("%w: render helm release values: %v", apperrors.ErrClusterUnready, err)
	}
	return domainresource.HelmValuesView{
		Name: strings.TrimSpace(releaseV1.Name), Namespace: strings.TrimSpace(releaseV1.Namespace),
		Revision: strconv.Itoa(releaseV1.Version), Content: content, Original: content,
		Editable: false, DiffEnabled: true,
	}, nil
}

func (d *Direct) UpdateHelmReleaseValues(ctx context.Context, clusterID, namespace, name, content string) (domainresource.HelmValuesView, error) {
	values, err := parseHelmValues(content)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	actionConfig, err := d.helmActionConfig(ctx, clusterID, namespace)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	current, err := action.NewGet(actionConfig).Run(name)
	if err != nil {
		return domainresource.HelmValuesView{}, mapHelmReleaseSDKError(name, "get helm release", err)
	}
	currentV1, err := helmSDKReleaseV1(current)
	if err != nil {
		return domainresource.HelmValuesView{}, mapHelmReleaseSDKError(name, "read helm release", err)
	}
	if currentV1 == nil || currentV1.Chart == nil {
		return domainresource.HelmValuesView{}, fmt.Errorf("%w: helm release %s has no chart payload", apperrors.ErrClusterUnready, name)
	}
	upgrader := action.NewUpgrade(actionConfig)
	upgrader.Namespace = namespace
	upgrader.ResetValues = true
	upgrader.WaitStrategy = kube.LegacyStrategy
	upgrader.WaitForJobs = true
	upgrader.Timeout = directHelmTimeoutSeconds * time.Second
	release, err := upgrader.RunWithContext(ctx, name, currentV1.Chart, values)
	if err != nil {
		return domainresource.HelmValuesView{}, mapHelmReleaseSDKError(name, "update helm release values", err)
	}
	releaseV1, err := helmSDKReleaseV1(release)
	if err != nil {
		return domainresource.HelmValuesView{}, mapHelmReleaseSDKError(name, "read updated helm release values", err)
	}
	if releaseV1 == nil {
		return domainresource.HelmValuesView{}, fmt.Errorf("%w: update helm release values returned no release", apperrors.ErrClusterUnready)
	}
	return domainresource.HelmValuesView{
		Name: strings.TrimSpace(releaseV1.Name), Namespace: strings.TrimSpace(releaseV1.Namespace),
		Revision: strconv.Itoa(releaseV1.Version), Content: content, Original: content,
		Editable: true, DiffEnabled: true,
	}, nil
}

func (d *Direct) DeleteHelmRelease(ctx context.Context, clusterID, namespace, name string) error {
	actionConfig, err := d.helmActionConfig(ctx, clusterID, namespace)
	if err != nil {
		return err
	}
	uninstaller := action.NewUninstall(actionConfig)
	uninstaller.WaitStrategy = kube.LegacyStrategy
	uninstaller.Timeout = directHelmTimeoutSeconds * time.Second
	if _, err := uninstaller.Run(name); err != nil {
		return mapHelmReleaseSDKError(name, "delete helm release", err)
	}
	return nil
}

func (d *Direct) InstallHelmChart(ctx context.Context, clusterID string, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	values, err := parseHelmValues(input.ValuesYAML)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	settings, err := newHelmEnvSettings(input.Namespace)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	actionConfig, err := d.helmActionConfig(ctx, clusterID, input.Namespace)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	if existing, ok, err := existingHelmInstallResult(actionConfig, input); err != nil {
		return domainresource.HelmChartInstallResult{}, err
	} else if ok {
		return existing, nil
	}

	installer := action.NewInstall(actionConfig)
	installer.ReleaseName = input.ReleaseName
	installer.Namespace = input.Namespace
	installer.CreateNamespace = input.CreateNamespace
	installer.WaitForJobs = input.Wait
	installer.ServerSideApply = false
	installer.WaitStrategy = kube.HookOnlyStrategy
	if input.Wait {
		installer.WaitStrategy = kube.LegacyStrategy
	}
	installer.Timeout = time.Duration(input.TimeoutSeconds) * time.Second
	installer.DependencyUpdate = true
	installer.RepoURL = input.RepositoryURL
	installer.Version = input.Version

	chartPath, err := installer.LocateChart(input.ChartName, settings)
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
			if existing, ok, lookupErr := existingHelmInstallResult(actionConfig, input); lookupErr != nil {
				return domainresource.HelmChartInstallResult{}, lookupErr
			} else if ok {
				return existing, nil
			}
			return domainresource.HelmChartInstallResult{}, helmReleaseNameUnavailableError(input.ReleaseName, input.Namespace, "", "")
		}
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: install helm chart: %v", apperrors.ErrClusterUnready, err)
	}
	releaseV1, err := helmSDKReleaseV1(release)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, fmt.Errorf("%w: read installed helm release: %v", apperrors.ErrClusterUnready, err)
	}
	return mapHelmChartInstallResult(releaseV1), nil
}

func (d *Direct) helmActionConfig(ctx context.Context, clusterID, namespace string) (*action.Configuration, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	actionConfig := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
	getter := helmRESTClientGetter{restConfig: bundle.RESTConfig, namespace: namespace}
	if err := actionConfig.Init(getter, namespace, "secrets"); err != nil {
		return nil, fmt.Errorf("%w: initialize helm action: %v", apperrors.ErrClusterUnready, err)
	}
	return actionConfig, nil
}

func existingHelmInstallResult(actionConfig *action.Configuration, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, bool, error) {
	release, err := action.NewGet(actionConfig).Run(input.ReleaseName)
	if err != nil {
		if isHelmReleaseNotFoundError(err) {
			return domainresource.HelmChartInstallResult{}, false, nil
		}
		return domainresource.HelmChartInstallResult{}, false, fmt.Errorf("%w: inspect existing helm release: %v", apperrors.ErrClusterUnready, err)
	}
	releaseV1, err := helmSDKReleaseV1(release)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, false, fmt.Errorf("%w: read existing helm release: %v", apperrors.ErrClusterUnready, err)
	}
	if helmSDKReleaseSatisfiesInstall(releaseV1, input) {
		result := mapHelmChartInstallResult(releaseV1)
		result.Description = firstNonEmptyHelm(result.Description, "Release already deployed; install request already satisfied")
		return result, true, nil
	}
	status, revision := "", ""
	if releaseV1 != nil {
		if releaseV1.Info != nil {
			status = strings.TrimSpace(string(releaseV1.Info.Status))
		}
		if releaseV1.Version > 0 {
			revision = strconv.Itoa(releaseV1.Version)
		}
	}
	return domainresource.HelmChartInstallResult{}, false, helmReleaseNameUnavailableError(input.ReleaseName, input.Namespace, status, revision)
}

func helmReleaseNameUnavailableError(releaseName, namespace, status, revision string) error {
	parts := []string{fmt.Sprintf("releaseName %q in namespace %q is already used by Helm release history", strings.TrimSpace(releaseName), strings.TrimSpace(namespace))}
	if status != "" {
		parts = append(parts, fmt.Sprintf("status %q", status))
	}
	if revision != "" {
		parts = append(parts, fmt.Sprintf("revision %s", revision))
	}
	return fmt.Errorf("%w: %s; choose another release name or uninstall the existing release before installing again", apperrors.ErrInvalidArgument, strings.Join(parts, ", "))
}

func helmSDKReleaseSatisfiesInstall(release *helmreleasev1.Release, input domainresource.HelmChartInstallInput) bool {
	if release == nil || release.Chart == nil || release.Chart.Metadata == nil {
		return false
	}
	status := ""
	if release.Info != nil {
		status = strings.TrimSpace(string(release.Info.Status))
	}
	return strings.EqualFold(status, "deployed") &&
		strings.EqualFold(strings.TrimSpace(release.Chart.Metadata.Name), strings.TrimSpace(input.ChartName)) &&
		strings.TrimSpace(release.Chart.Metadata.Version) == strings.TrimSpace(input.Version)
}

func isHelmReleaseNameInUseError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "cannot re-use a name that is still in use")
}

func mapHelmReleaseSDKError(name, operation string, err error) error {
	if isHelmReleaseNotFoundError(err) {
		return fmt.Errorf("%w: helm release %s not found", apperrors.ErrNotFound, name)
	}
	return fmt.Errorf("%w: %s: %v", apperrors.ErrClusterUnready, operation, err)
}

func isHelmReleaseNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrReleaseNotFound) || errors.Is(err, driver.ErrNoDeployedReleases) {
		return true
	}
	normalized := strings.ToLower(err.Error())
	return strings.Contains(normalized, "release: not found") || strings.Contains(normalized, "not found")
}

func helmSDKReleaseV1(release helmreleasepkg.Releaser) (*helmreleasev1.Release, error) {
	switch typed := release.(type) {
	case nil:
		return nil, nil
	case helmreleasev1.Release:
		return &typed, nil
	case *helmreleasev1.Release:
		return typed, nil
	default:
		return nil, fmt.Errorf("unsupported helm release type %T", release)
	}
}

func parseHelmValues(content string) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	if strings.TrimSpace(content) == "" {
		return values, nil
	}
	if err := yaml.Unmarshal([]byte(content), &values); err != nil {
		return nil, fmt.Errorf("%w: invalid values yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	if values == nil {
		return map[string]interface{}{}, nil
	}
	return values, nil
}

func newHelmEnvSettings(namespace string) (*cli.EnvSettings, error) {
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
		Clusters:  map[string]*clientcmdapi.Cluster{"cluster": {Server: ""}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"user": {}},
		Contexts: map[string]*clientcmdapi.Context{
			"context": {Cluster: "cluster", AuthInfo: "user", Namespace: namespace},
		},
		CurrentContext: "context",
	}
	return clientcmd.NewDefaultClientConfig(rawConfig, &clientcmd.ConfigOverrides{Context: clientcmdapi.Context{Namespace: namespace}})
}

var _ appresource.DirectHelm = (*Direct)(nil)
