package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/engine"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kuberesource "k8s.io/cli-runtime/pkg/resource"
	restfake "k8s.io/client-go/rest/fake"
)

type observedHelmKubeClient struct {
	kubefake.PrintingKubeClient
	deleting  bool
	missing   bool
	namespace string
	reads     int
	uid       string
}

func (c *observedHelmKubeClient) Build(io.Reader, bool) (kube.ResourceList, error) {
	object := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app","namespace":"dev"}}`
	client := &restfake.RESTClient{GroupVersion: schema.GroupVersion{Version: "v1"}, NegotiatedSerializer: kuberesource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer, Client: restfake.CreateHTTPClient(func(*http.Request) (*http.Response, error) {
		c.reads++
		if c.missing {
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404}`))}, nil
		}
		body := object
		if c.uid != "" {
			body = strings.Replace(body, `"name":"app"`, `"name":"app","uid":"`+c.uid+`"`, 1)
		}
		if c.deleting {
			body = strings.Replace(body, `"name":"app"`, `"name":"app","deletionTimestamp":"2026-09-12T00:00:00Z"`, 1)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	var item unstructured.Unstructured
	if err := json.Unmarshal([]byte(object), &item); err != nil {
		return nil, err
	}
	namespace := "dev"
	if c.namespace != "" {
		namespace = c.namespace
	}
	return kube.ResourceList{&kuberesource.Info{Client: client, Namespace: namespace, Name: "app", Object: &item, Mapping: &meta.RESTMapping{Resource: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Scope: meta.RESTScopeNamespace}}}, nil
}

func TestHelmObservationUsesLiveResourceHealth(t *testing.T) {
	client := &observedHelmKubeClient{}
	cfg := &action.Configuration{KubeClient: client}
	snapshot := sohaapi.HelmDeliverySnapshot{ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "dev", TargetID: "target", DeliveryPlanID: "plan", ReleaseName: "app", Namespace: "dev"}
	_, digest, err := helmDeliveryResources(cfg, "dev", "manifest", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.RenderedDigest = digest
	release := &releasev1.Release{Name: "app", Namespace: "dev", Version: 1, Manifest: "manifest", Labels: helmDeliveryLabels(snapshot), Info: &releasev1.Info{Status: releasecommon.StatusDeployed}}
	for _, deleting := range []bool{false, true} {
		client.deleting = deleting
		result, err := observeHelmDelivery(context.Background(), cfg, snapshot, release)
		if err != nil || result.Ready == deleting || len(result.Resources) != 1 {
			t.Fatalf("live health deleting=%v: %+v %v", deleting, result, err)
		}
	}
}

func TestHelmDeliveryFrozenRenderAndNativeRollbackRecovery(t *testing.T) {
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  literal: '{{ this is data }}'\n"
	hook := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: hook\n  annotations:\n    helm.sh/hook: pre-install,pre-upgrade\n"
	original := helmSourceTestArchive(t)
	frozen, err := helmrelease.FrozenChartArchive(original, manifest, []string{hook})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := loader.LoadArchive(bytes.NewReader(frozen))
	if err != nil {
		t.Fatal(err)
	}
	files, err := engine.Render(ch, map[string]any{})
	if err != nil || files["app/templates/resources.yaml"] != manifest || files["app/templates/hook-0.yaml"] != hook {
		t.Fatalf("frozen render changed approved resources: %v %#v", err, files)
	}
	cfg := &action.Configuration{Releases: storage.Init(driver.NewMemory()), KubeClient: &kubefake.PrintingKubeClient{Out: io.Discard}, Capabilities: common.DefaultCapabilities}
	snapshot := sohaapi.HelmDeliverySnapshot{ClusterID: "cluster", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "dev", TargetID: "target", DeliveryPlanID: "old-plan", ReleaseName: "app", Namespace: "dev", ChartDigest: helmrelease.SHA256(original)}
	nativeChart, ok := ch.(*chartv2.Chart)
	if !ok {
		t.Fatal("frozen chart has an unexpected type")
	}
	for version := 1; version <= 2; version++ {
		item := &releasev1.Release{Name: "app", Namespace: "dev", Version: version, Info: &releasev1.Info{Status: releasecommon.StatusDeployed}, Labels: helmDeliveryLabels(snapshot), Chart: nativeChart, Config: map[string]any{"version": version}, Manifest: manifest}
		if err := cfg.Releases.Create(item); err != nil {
			t.Fatal(err)
		}
	}
	snapshot.DeliveryPlanID, snapshot.ExpectedRevision, snapshot.RollbackRevision = "rollback-plan", 2, 1
	if err := RequireUnmanaged(cfg, snapshot.ReleaseName); err == nil {
		t.Fatal("legacy mutation accepted a governed release")
	}
	if err := RequireUnmanaged(cfg, "missing-release"); err != nil {
		t.Fatal("ownership guard replaced normal missing-release handling", err)
	}
	snapshot.Operation = sohaapi.HelmDeliverySnapshotOperationRollback
	_, snapshot.RenderedDigest, err = helmDeliveryResources(cfg, "dev", manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := runHelmDeliveryRollback(cfg, snapshot, time.Second)
	if err != nil || rolledBack.Version != 3 || rolledBack.Config["version"] != 1 || !helmDeliveryAlreadyApplied(rolledBack, snapshot) {
		t.Fatalf("native rollback did not produce the selected revision with new plan ownership: %+v %v", rolledBack, err)
	}
	old, err := currentHelmDelivery(cfg, "app", 1)
	if err != nil || old.Labels["soha-delivery-plan"] == rolledBack.Labels["soha-delivery-plan"] {
		t.Fatalf("rollback rewrote previous history: %v", err)
	}
	prepared := &sohaapi.HelmPreparedRelease{Manifest: manifest}
	if err := json.Unmarshal([]byte(`{"version":1}`), &prepared.Values); err != nil {
		t.Fatal(err)
	}
	snapshot.ValuesDigest = helmrelease.SHA256([]byte(`{"version":1}`))
	result, err := Execute(context.Background(), cfg, sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot, Prepared: prepared})
	if err != nil || !result.Ready || result.Revision != 3 {
		t.Fatalf("recovered callback repeated or lost native rollback: %+v %v", result, err)
	}
	history, err := cfg.Releases.History("app")
	if err != nil || len(history) != 3 {
		t.Fatalf("retry created another native revision: %d %v", len(history), err)
	}
	old.Config["version"] = 99
	if err := cfg.Releases.Update(old); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), cfg, sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot, Prepared: prepared}); err == nil {
		t.Fatal("rollback accepted changed historical values")
	}
	rolledBack.Info.Status = releasecommon.StatusFailed
	result, err = observeHelmDelivery(context.Background(), cfg, snapshot, rolledBack)
	if err == nil || result.Ready || !result.Stopped || result.Revision != 3 || result.Status != "failed" {
		t.Fatalf("failed side effect disappeared: %+v %v", result, err)
	}
}

func helmSourceTestArchive(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zip := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zip)
	for name, content := range map[string]string{
		"app/Chart.yaml":         "apiVersion: v2\nname: app\nversion: 1.2.3\n",
		"app/values.yaml":        "replicaCount: 1\n",
		"app/values.schema.json": `{"type":"object","properties":{"replicaCount":{"type":"integer","minimum":1}}}`,
	} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestHelmPrepareKeepsLiveClientsForResourceValidation(t *testing.T) {
	client := &observedHelmKubeClient{missing: true}
	releases := storage.Init(driver.NewMemory())
	cfg := &action.Configuration{KubeClient: client, Releases: releases, Capabilities: common.DefaultCapabilities}
	archive := helmSourceTestArchive(t)
	input := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Preflight, Snapshot: sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "plan", ApplicationID: "app", ServiceID: "svc", ServiceVersion: 1, ApplicationEnvironmentID: "dev", TargetID: "target", ClusterID: "cluster", Namespace: "dev", ReleaseName: "app", ChartDigest: helmrelease.SHA256(archive)}, Prepared: &sohaapi.HelmPreparedRelease{ChartArchive: base64.StdEncoding.EncodeToString(archive)}}
	result, err := Prepare(context.Background(), cfg, input)
	if err != nil || len(result.Snapshot.Resources) != 1 || cfg.KubeClient != client || cfg.Releases != releases {
		t.Fatalf("prepare lost live validation: resources=%d client=%T err=%v", len(result.Snapshot.Resources), cfg.KubeClient, err)
	}
	if _, err := releases.Last("app"); err == nil {
		t.Fatal("preparation persisted a release")
	}
}

func TestHelmPreflightUsesClusterCapabilitiesWithoutTemplateLookup(t *testing.T) {
	client := &observedHelmKubeClient{missing: true}
	caps := common.DefaultCapabilities.Copy()
	caps.KubeVersion = common.KubeVersion{Version: "v1.36.0", Major: "1", Minor: "36"}
	caps.APIVersions = common.VersionSet{"v1"}
	lookups := 0
	cfg := &action.Configuration{KubeClient: client, Releases: storage.Init(driver.NewMemory()), Capabilities: caps, CustomTemplateFuncs: map[string]any{"lookup": func(string, string, string, string) (map[string]any, error) {
		lookups++
		return map[string]any{"secret": "private"}, nil
	}}}
	ch := &chartv2.Chart{Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "app", Version: "1.0.0", KubeVersion: ">=1.35.0"}, Templates: []*common.File{{Name: "templates/config.yaml", Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  version: {{ .Capabilities.KubeVersion.Version | quote }}\n  absentAPI: {{ .Capabilities.APIVersions.Has \"apps/v1\" | quote }}\n  lookup: {{ lookup \"v1\" \"Secret\" \"other-namespace\" \"private\" | toJson | quote }}\n")}}}
	input := sohaapi.HelmExecutionTaskPayload{Snapshot: sohaapi.HelmDeliverySnapshot{Operation: sohaapi.HelmDeliverySnapshotOperationInstall, ReleaseName: "app", Namespace: "dev"}}
	result, err := runHelmDeliveryAction(context.Background(), cfg, input, ch, true)
	if err != nil {
		t.Fatal(err)
	}
	if lookups != 0 || !strings.Contains(result.Manifest, `version: "v1.36.0"`) || !strings.Contains(result.Manifest, `absentAPI: "false"`) || !strings.Contains(result.Manifest, `lookup: "{}"`) {
		t.Fatal("preflight ignored actual capabilities or read template data", result.Manifest)
	}
	lookup, ok := cfg.CustomTemplateFuncs["lookup"].(func(string, string, string, string) (map[string]any, error))
	if !ok {
		t.Fatal("preflight replaced the caller's template functions")
	}
	if original, err := lookup("", "", "", ""); err != nil || original["secret"] != "private" {
		t.Fatal("preflight changed the caller's lookup function")
	}
	if _, err := cfg.Releases.Last("app"); err == nil {
		t.Fatal("preflight persisted a release")
	}
	client.namespace, client.reads = "other-namespace", 0
	if _, err := runHelmDeliveryAction(context.Background(), cfg, input, ch, true); err == nil || client.reads != 0 {
		t.Fatal("preflight read a resource outside the authorized namespace", err, client.reads)
	}
}
