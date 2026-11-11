// Package runtime executes the versioned Helm task contract using native Helm actions.
// Callers supply an authorized cluster connection; policy, queues and credentials stay with callers.
package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	contractresource "github.com/opensoha/soha-contracts/resource"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/release"
	helmreleasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

func Prepare(ctx context.Context, cfg *action.Configuration, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	if err := helmrelease.ValidateTaskIdentity(input); err != nil {
		return input, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	current, err := currentHelmDelivery(cfg, input.Snapshot.ReleaseName, 0)
	if err != nil {
		return input, err
	}
	if helmrelease.IsInitialization(input.Snapshot) && helmDeliveryAlreadyApplied(current, input.Snapshot) {
		if err := validateHelmPrepared(cfg, input); err != nil {
			return input, err
		}
		_, err := observeInitializedHelm(ctx, cfg, input, current)
		return input, err
	}
	input.Snapshot.ExpectedRevision = 0
	input.Snapshot.Operation = sohaapi.HelmDeliverySnapshotOperationInstall
	if current != nil {
		if current.Labels["soha-delivery-owner"] != helmDeliveryOwner(input.Snapshot) || current.Info == nil || strings.HasPrefix(string(current.Info.Status), "pending-") {
			return input, fmt.Errorf("%w: Helm release belongs to another owner", ErrConflict)
		}
		input.Snapshot.ExpectedRevision = current.Version
		input.Snapshot.Operation = sohaapi.HelmDeliverySnapshotOperationUpgrade
	}
	var rendered *helmreleasev1.Release
	if input.Snapshot.RollbackRevision > 0 {
		if current == nil {
			return input, fmt.Errorf("%w: Helm rollback requires an installed release", ErrConflict)
		}
		rendered, err = currentHelmDelivery(cfg, input.Snapshot.ReleaseName, input.Snapshot.RollbackRevision)
		if err == nil && (rendered == nil || rendered.Labels["soha-delivery-owner"] != helmDeliveryOwner(input.Snapshot)) {
			err = fmt.Errorf("%w: Helm rollback revision is not owned by this service", ErrConflict)
		}
		input.Snapshot.Operation = sohaapi.HelmDeliverySnapshotOperationRollback
		if err == nil {
			input.Snapshot.ChartDigest = "sha256:" + rendered.Labels["soha-chart-sha-a"] + rendered.Labels["soha-chart-sha-b"]
			if len(input.Snapshot.ChartDigest) != 71 || (rendered.Chart == nil || rendered.Chart.Metadata == nil) {
				err = fmt.Errorf("%w: rollback revision lacks a verified chart origin", ErrConflict)
			} else {
				input.Snapshot.Chart, input.Snapshot.ChartVersion = rendered.Chart.Metadata.Name, rendered.Chart.Metadata.Version
			}
		}
	} else {
		var ch chart.Charter
		ch, err = preparedHelmChart(input, false)
		if err == nil {
			rendered, err = runHelmDeliveryAction(ctx, cfg, input, ch, true)
		}
	}
	if err != nil {
		return input, fmt.Errorf("%w: Helm rendering or preflight failed", ErrInvalidArgument)
	}
	if input.Prepared == nil {
		input.Prepared = &sohaapi.HelmPreparedRelease{}
	}
	input.Prepared.Manifest, input.Prepared.Hooks = rendered.Manifest, helmDeliveryHooks(rendered)
	values, err := json.Marshal(rendered.Config)
	if err != nil {
		return input, err
	}
	if err := json.Unmarshal(values, &input.Prepared.Values); err != nil {
		return input, err
	}
	input.Snapshot.ValuesDigest = helmrelease.SHA256(values)
	input.Snapshot.Resources, input.Snapshot.RenderedDigest, err = helmDeliveryResources(cfg, input.Snapshot.Namespace, rendered.Manifest, input.Prepared.Hooks)
	if err == nil {
		err = helmrelease.ValidateTaskIdentity(input)
	}
	return input, err
}

func Execute(ctx context.Context, cfg *action.Configuration, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	if err := helmrelease.ValidateTaskIdentity(input); err != nil {
		return sohaapi.HelmExecutionTaskResult{Stopped: true}, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	result := sohaapi.HelmExecutionTaskResult{Stopped: true, RenderedDigest: input.Snapshot.RenderedDigest, Resources: []sohaapi.ManifestResourceInventory{}}
	current, err := currentHelmDelivery(cfg, input.Snapshot.ReleaseName, 0)
	if err != nil {
		return result, err
	}
	if input.Action == sohaapi.Observe {
		return observeInitializedHelm(ctx, cfg, input, current)
	}
	if err := validateHelmPrepared(cfg, input); err != nil {
		return result, err
	}
	if helmDeliveryAlreadyApplied(current, input.Snapshot) {
		return observeInitializedHelm(ctx, cfg, input, current)
	}
	if err := helmDeliveryRevisionMatches(current, input.Snapshot); err != nil {
		return result, err
	}
	if helmrelease.IsInitialization(input.Snapshot) {
		if err := requireInitialHelmResourcesAbsent(cfg, input); err != nil {
			return result, err
		}
	}
	if input.Action == sohaapi.Preflight {
		result.Ready, result.Status, result.Revision = true, "preflighted", input.Snapshot.ExpectedRevision
		return result, nil
	}
	if input.Action != sohaapi.Apply {
		return result, fmt.Errorf("%w: unsupported Helm delivery action", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var ch chart.Charter
	if input.Snapshot.Operation != sohaapi.HelmDeliverySnapshotOperationRollback {
		ch, err = preparedHelmChart(input, true)
		if err != nil {
			return result, err
		}
	}
	// Helm's RunWithContext may return while its mutation goroutine is still
	// running. Once mutation begins, native Timeout bounds it; cancellation is
	// acknowledged only after Run has returned and the release is inspected.
	var initialization *initializingHelmClient
	if helmrelease.IsInitialization(input.Snapshot) {
		initialization = &initializingHelmClient{Interface: cfg.KubeClient, created: make(map[string]types.UID)}
		cfg = helmConfigurationWithClient(cfg, initialization)
	}
	_, runErr := runHelmDeliveryAction(context.Background(), cfg, input, ch, false)
	current, err = currentHelmDelivery(cfg, input.Snapshot.ReleaseName, 0)
	if err == nil && current != nil {
		result, err = observeHelmDelivery(ctx, cfg, input.Snapshot, current)
		if err == nil && initialization != nil {
			err = initialization.validateCreated(result)
		}
	}
	if err != nil || runErr != nil {
		result.Ready = false
	}
	if runErr != nil {
		return result, fmt.Errorf("%w: Helm execution failed; inspect the recorded revision and resource state", ErrUnavailable)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}

// Build a per-invocation configuration without copying Helm's internal mutex or
// replacing the caller's client. Dry-run template functions also remain local.
func helmConfigurationWithClient(original *action.Configuration, client kube.Interface) *action.Configuration {
	cfg := &action.Configuration{
		RESTClientGetter: original.RESTClientGetter, Releases: original.Releases,
		KubeClient: client, RegistryClient: original.RegistryClient,
		Capabilities: original.Capabilities, HookOutputFunc: original.HookOutputFunc,
		CustomTemplateFuncs: maps.Clone(original.CustomTemplateFuncs),
	}
	cfg.SetLogger(original.Logger().Handler())
	return cfg
}

func runHelmDeliveryAction(ctx context.Context, cfg *action.Configuration, input sohaapi.HelmExecutionTaskPayload, ch chart.Charter, dryRun bool) (*helmreleasev1.Release, error) {
	if dryRun {
		// Discover real cluster capabilities, but keep template lookup disabled.
		cfg = helmConfigurationWithClient(cfg, cfg.KubeClient)
		if cfg.CustomTemplateFuncs == nil {
			cfg.CustomTemplateFuncs = make(map[string]any)
		}
		cfg.CustomTemplateFuncs["lookup"] = func(string, string, string, string) (map[string]any, error) { return map[string]any{}, nil }
	}
	values := map[string]any{}
	if input.Prepared != nil {
		encoded, err := json.Marshal(input.Prepared.Values)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &values); err != nil {
			return nil, err
		}
	}
	timeout := time.Duration(input.Snapshot.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	labels := helmDeliveryLabels(input.Snapshot)
	if input.Snapshot.Operation == sohaapi.HelmDeliverySnapshotOperationRollback {
		return runHelmDeliveryRollback(cfg, input.Snapshot, timeout)
	}
	if input.Snapshot.Operation == sohaapi.HelmDeliverySnapshotOperationInstall {
		install := action.NewInstall(cfg)
		install.ReleaseName, install.Namespace = input.Snapshot.ReleaseName, input.Snapshot.Namespace
		install.ServerSideApply, install.CreateNamespace = false, false
		install.Timeout, install.WaitStrategy, install.WaitForJobs = timeout, kube.LegacyStrategy, true
		install.Labels, install.Description = labels, "Soha delivery "+input.Snapshot.DeliveryPlanID
		if dryRun {
			install.DryRunStrategy = action.DryRunServer
			install.PostRenderer = helmPreflightScope{cfg: cfg, namespace: input.Snapshot.Namespace}
		}
		release, err := install.RunWithContext(ctx, ch, values)
		if err != nil {
			return nil, err
		}
		return helmSDKReleaseV1(release)
	}
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace, upgrade.ResetValues = input.Snapshot.Namespace, true
	upgrade.ServerSideApply = "false"
	upgrade.Timeout, upgrade.WaitStrategy, upgrade.WaitForJobs = timeout, kube.LegacyStrategy, true
	upgrade.Labels, upgrade.Description = labels, "Soha delivery "+input.Snapshot.DeliveryPlanID
	if dryRun {
		upgrade.DryRunStrategy = action.DryRunServer
		upgrade.PostRenderer = helmPreflightScope{cfg: cfg, namespace: input.Snapshot.Namespace}
	}
	release, err := upgrade.RunWithContext(ctx, input.Snapshot.ReleaseName, ch, values)
	if err != nil {
		return nil, err
	}
	return helmSDKReleaseV1(release)
}

// Validate before Helm's ownership checks can read any rendered resource.
type helmPreflightScope struct {
	cfg       *action.Configuration
	namespace string
}

func (p helmPreflightScope) Run(manifest *bytes.Buffer) (*bytes.Buffer, error) {
	_, _, err := helmDeliveryResources(p.cfg, p.namespace, manifest.String(), nil)
	return manifest, err
}

func preparedHelmChart(input sohaapi.HelmExecutionTaskPayload, frozen bool) (chart.Charter, error) {
	if input.Prepared == nil {
		return nil, fmt.Errorf("%w: confidential Helm preparation is missing", ErrInvalidArgument)
	}
	archive, err := base64.StdEncoding.DecodeString(input.Prepared.ChartArchive)
	if err != nil || helmrelease.SHA256(archive) != input.Snapshot.ChartDigest {
		return nil, fmt.Errorf("%w: Helm chart snapshot digest mismatch", ErrConflict)
	}
	if err := helmrelease.ValidateChartArchive(archive); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	if frozen {
		archive, err = helmrelease.FrozenChartArchive(archive, input.Prepared.Manifest, input.Prepared.Hooks)
		if err != nil {
			return nil, err
		}
	}
	return loader.LoadArchive(bytes.NewReader(archive))
}

func validateHelmPrepared(cfg *action.Configuration, input sohaapi.HelmExecutionTaskPayload) error {
	if input.Prepared == nil {
		return fmt.Errorf("%w: confidential Helm preparation is missing", ErrInvalidArgument)
	}
	values, err := json.Marshal(input.Prepared.Values)
	if err != nil || helmrelease.SHA256(values) != input.Snapshot.ValuesDigest {
		return fmt.Errorf("%w: Helm values snapshot changed", ErrConflict)
	}
	_, digest, err := helmDeliveryResources(cfg, input.Snapshot.Namespace, input.Prepared.Manifest, input.Prepared.Hooks)
	if err != nil {
		return err
	}
	if digest != input.Snapshot.RenderedDigest {
		return fmt.Errorf("%w: Helm rendered resources changed", ErrConflict)
	}
	if input.Snapshot.Operation == sohaapi.HelmDeliverySnapshotOperationRollback {
		selected, err := currentHelmDelivery(cfg, input.Snapshot.ReleaseName, input.Snapshot.RollbackRevision)
		if err != nil || selected == nil || selected.Labels["soha-delivery-owner"] != helmDeliveryOwner(input.Snapshot) {
			return fmt.Errorf("%w: Helm rollback revision is unavailable", ErrConflict)
		}
		_, selectedDigest, err := helmDeliveryResources(cfg, input.Snapshot.Namespace, selected.Manifest, helmDeliveryHooks(selected))
		if err != nil || selectedDigest != digest {
			return fmt.Errorf("%w: Helm rollback revision differs from the approved render", ErrConflict)
		}
		selectedValues, err := json.Marshal(selected.Config)
		if err != nil || helmrelease.SHA256(selectedValues) != input.Snapshot.ValuesDigest || "sha256:"+selected.Labels["soha-chart-sha-a"]+selected.Labels["soha-chart-sha-b"] != input.Snapshot.ChartDigest {
			return fmt.Errorf("%w: Helm rollback values or chart origin changed", ErrConflict)
		}
	}
	return nil
}

func runHelmDeliveryRollback(cfg *action.Configuration, snapshot sohaapi.HelmDeliverySnapshot, timeout time.Duration) (*helmreleasev1.Release, error) {
	// Native rollback reuses the old labels. Decorate only the newly created
	// revision so retry recovery recognizes this plan without rewriting history.
	original := cfg.Releases.Driver
	cfg.Releases.Driver = helmRollbackDriver{Driver: original, snapshot: snapshot}
	defer func() { cfg.Releases.Driver = original }()
	rollback := action.NewRollback(cfg)
	rollback.Version, rollback.ServerSideApply = snapshot.RollbackRevision, "false"
	rollback.Timeout, rollback.WaitStrategy, rollback.WaitForJobs = timeout, kube.LegacyStrategy, true
	if err := rollback.Run(snapshot.ReleaseName); err != nil {
		return nil, err
	}
	return currentHelmDelivery(cfg, snapshot.ReleaseName, 0)
}

type helmRollbackDriver struct {
	driver.Driver
	snapshot sohaapi.HelmDeliverySnapshot
}

func (d helmRollbackDriver) decorate(value release.Releaser) error {
	item, err := helmSDKReleaseV1(value)
	if err != nil {
		return err
	}
	if item.Name == d.snapshot.ReleaseName && item.Version == d.snapshot.ExpectedRevision+1 {
		labels := map[string]string{}
		for key, value := range item.Labels {
			labels[key] = value
		}
		for key, value := range helmDeliveryLabels(d.snapshot) {
			labels[key] = value
		}
		item.Labels = labels
	}
	return nil
}

func (d helmRollbackDriver) Create(key string, value release.Releaser) error {
	if err := d.decorate(value); err != nil {
		return err
	}
	return d.Driver.Create(key, value)
}

func (d helmRollbackDriver) Update(key string, value release.Releaser) error {
	if err := d.decorate(value); err != nil {
		return err
	}
	return d.Driver.Update(key, value)
}

func currentHelmDelivery(cfg *action.Configuration, name string, revision int) (*helmreleasev1.Release, error) {
	get := action.NewGet(cfg)
	get.Version = revision
	release, err := get.Run(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read Helm release state", ErrUnavailable)
	}
	return helmSDKReleaseV1(release)
}

// RequireUnmanaged prevents legacy Helm mutation endpoints bypassing delivery plans.
func RequireUnmanaged(cfg *action.Configuration, name string) error {
	current, err := currentHelmDelivery(cfg, name, 0)
	if err != nil {
		return err
	}
	if current != nil && current.Labels["soha-delivery-owner"] != "" {
		return fmt.Errorf("%w: application Helm releases are managed through delivery plans", ErrConflict)
	}
	return nil
}

func helmDeliveryOwner(snapshot sohaapi.HelmDeliverySnapshot) string {
	return helmrelease.SHA256([]byte(snapshot.ApplicationID + "/" + snapshot.ServiceID + "/" + snapshot.ApplicationEnvironmentID + "/" + snapshot.TargetID))[7:39]
}

func helmDeliveryLabels(snapshot sohaapi.HelmDeliverySnapshot) map[string]string {
	labels := map[string]string{"soha-delivery-owner": helmDeliveryOwner(snapshot), "soha-delivery-plan": helmrelease.SHA256([]byte(snapshot.DeliveryPlanID))[7:39]}
	if len(snapshot.ChartDigest) == 71 {
		labels["soha-chart-sha-a"], labels["soha-chart-sha-b"] = snapshot.ChartDigest[7:39], snapshot.ChartDigest[39:]
	}
	return labels
}

func helmDeliveryAlreadyApplied(release *helmreleasev1.Release, snapshot sohaapi.HelmDeliverySnapshot) bool {
	return release != nil && release.Version == snapshot.ExpectedRevision+1 && release.Labels["soha-delivery-plan"] == helmDeliveryLabels(snapshot)["soha-delivery-plan"] && release.Labels["soha-delivery-owner"] == helmDeliveryOwner(snapshot) && release.Info != nil && string(release.Info.Status) == "deployed"
}

func helmDeliveryRevisionMatches(release *helmreleasev1.Release, snapshot sohaapi.HelmDeliverySnapshot) error {
	if release == nil && snapshot.ExpectedRevision == 0 && snapshot.Operation == sohaapi.HelmDeliverySnapshotOperationInstall {
		return nil
	}
	if release == nil || release.Version != snapshot.ExpectedRevision || release.Labels["soha-delivery-owner"] != helmDeliveryOwner(snapshot) || release.Info == nil || strings.HasPrefix(string(release.Info.Status), "pending-") {
		return fmt.Errorf("%w: Helm release revision, ownership or operation state changed; create a new plan", ErrConflict)
	}
	return nil
}

func helmDeliveryHooks(release *helmreleasev1.Release) []string {
	hooks := make([]string, 0, len(release.Hooks))
	for _, hook := range release.Hooks {
		hooks = append(hooks, hook.Manifest)
	}
	return hooks
}

func helmDeliveryResources(cfg *action.Configuration, namespace, manifest string, hooks []string) ([]sohaapi.HelmDeliveryResource, string, error) {
	resources := []sohaapi.HelmDeliveryResource{}
	objects := map[string]any{}
	for index, content := range append([]string{manifest}, hooks...) {
		items, err := cfg.KubeClient.Build(strings.NewReader(content), true)
		if err != nil {
			return nil, "", fmt.Errorf("%w: Helm rendered resource validation failed", ErrInvalidArgument)
		}
		for _, item := range items {
			if len(resources) >= 300 || item.Name == "" || item.Mapping == nil || item.Mapping.Scope.Name() != meta.RESTScopeNameNamespace || item.Namespace != namespace {
				return nil, "", fmt.Errorf("%w: application Helm charts require namespaced resources within their target and at most 300 resources", ErrInvalidArgument)
			}
			accessor, err := meta.Accessor(item.Object)
			if err != nil || accessor.GetAnnotations()["helm.sh/resource-policy"] == "keep" {
				return nil, "", fmt.Errorf("%w: retained Helm resources cannot be safely reassigned by delivery plans", ErrInvalidArgument)
			}
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item.Object)
			if err != nil {
				return nil, "", err
			}
			identity := sohaapi.HelmDeliveryResource{APIVersion: item.Mapping.GroupVersionKind.GroupVersion().String(), Kind: item.Mapping.GroupVersionKind.Kind, Namespace: item.Namespace, Name: item.Name, Hook: index > 0}
			key := item.Mapping.GroupVersionKind.Group + "/" + identity.Kind + "/" + identity.Namespace + "/" + identity.Name
			if _, exists := objects[key]; exists {
				return nil, "", fmt.Errorf("%w: Helm chart contains duplicate resource identities", ErrInvalidArgument)
			}
			objects[key], resources = object, append(resources, identity)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return fmt.Sprint(resources[i]) < fmt.Sprint(resources[j]) })
	encoded, err := json.Marshal(objects)
	if err != nil {
		return nil, "", err
	}
	return resources, helmrelease.SHA256(encoded), nil
}

func observeHelmDelivery(ctx context.Context, cfg *action.Configuration, snapshot sohaapi.HelmDeliverySnapshot, release *helmreleasev1.Release) (sohaapi.HelmExecutionTaskResult, error) {
	result := sohaapi.HelmExecutionTaskResult{Stopped: true, RenderedDigest: snapshot.RenderedDigest, Resources: []sohaapi.ManifestResourceInventory{}}
	if release != nil {
		result.Revision = release.Version
		if release.Info != nil {
			result.Status = string(release.Info.Status)
		}
	}
	if !helmDeliveryAlreadyApplied(release, snapshot) {
		return result, fmt.Errorf("%w: expected Helm deployment is not complete", ErrConflict)
	}
	_, digest, err := helmDeliveryResources(cfg, snapshot.Namespace, release.Manifest, helmDeliveryHooks(release))
	if err != nil || digest != snapshot.RenderedDigest {
		return result, fmt.Errorf("%w: Helm release differs from the approved render", ErrConflict)
	}
	items, err := cfg.KubeClient.Build(strings.NewReader(release.Manifest), false)
	if err != nil {
		return result, err
	}
	ready := true
	for _, item := range items {
		if err := item.Get(); err != nil {
			return result, fmt.Errorf("%w: Helm resource observation failed", ErrUnavailable)
		}
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item.Object)
		if err != nil {
			return result, err
		}
		health, err := observeHelmObjectHealth(ctx, cfg, object)
		if err != nil {
			return result, fmt.Errorf("%w: Operator resource observation failed", ErrUnavailable)
		}
		accessor, err := meta.Accessor(item.Object)
		if err != nil {
			return result, err
		}
		result.Resources = append(result.Resources, sohaapi.ManifestResourceInventory{APIVersion: item.Mapping.GroupVersionKind.GroupVersion().String(), Kind: item.Mapping.GroupVersionKind.Kind, Namespace: item.Namespace, Name: item.Name, UID: string(accessor.GetUID()), ResourceVersion: accessor.GetResourceVersion(), Health: health})
		ready = ready && health == "healthy"
	}
	result.Ready = ready
	return result, nil
}

var (
	ErrConflict        = errors.New("helm delivery conflict")
	ErrInvalidArgument = errors.New("invalid Helm delivery")
	ErrUnavailable     = errors.New("helm runtime unavailable")
)

func helmSDKReleaseV1(value release.Releaser) (*helmreleasev1.Release, error) {
	switch item := value.(type) {
	case nil:
		return nil, nil
	case helmreleasev1.Release:
		return &item, nil
	case *helmreleasev1.Release:
		return item, nil
	default:
		return nil, fmt.Errorf("%w: unsupported native release format", ErrInvalidArgument)
	}
}

func observeHelmObjectHealth(ctx context.Context, cfg *action.Configuration, object map[string]any) (string, error) {
	item := &unstructured.Unstructured{Object: object}
	if item.GetAPIVersion() != "workloads.soha.io/v1alpha1" || item.GetKind() != "WorkloadCronJob" {
		return contractresource.ManifestHealth(object), nil
	}
	if cfg.RESTClientGetter == nil {
		return "unknown", ErrUnavailable
	}
	config, err := cfg.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return "unknown", err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return "unknown", err
	}
	return resourceruntime.ManifestHealth(ctx, client, item)
}
