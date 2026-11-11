package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/kube"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kuberesource "k8s.io/cli-runtime/pkg/resource"
	restfake "k8s.io/client-go/rest/fake"
)

func TestHelmInitializationLeavesCallerConfigurationUntouched(t *testing.T) {
	client := &observedHelmKubeClient{}
	original := &action.Configuration{KubeClient: client, Releases: storage.Init(driver.NewMemory()), CustomTemplateFuncs: map[string]any{"caller": "preserved"}}
	protected := &initializingHelmClient{Interface: client, created: make(map[string]types.UID)}
	local := helmConfigurationWithClient(original, protected)
	local.CustomTemplateFuncs["caller"] = "changed"
	if original.KubeClient != client || original.CustomTemplateFuncs["caller"] != "preserved" {
		t.Fatal("initialization changed the caller's Kubernetes client or template functions")
	}
	if local.KubeClient != protected || local.Releases != original.Releases {
		t.Fatal("per-invocation configuration lost the protected client or release storage")
	}
}

func TestHelmInitializationObserveAndPrepareRecoveryRequireOriginalReceipt(t *testing.T) {
	client := &observedHelmKubeClient{missing: true}
	cfg := &action.Configuration{KubeClient: client, Releases: storage.Init(driver.NewMemory()), Capabilities: common.DefaultCapabilities}
	archive := helmSourceTestArchive(t)
	input := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Preflight, Snapshot: sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "first", TargetID: "target", ApplicationID: "app", ApplicationEnvironmentID: "dev", ServiceID: "svc", ClusterID: "cluster", Namespace: "dev", ReleaseName: "app", ChartDigest: helmrelease.SHA256(archive)}, Prepared: &sohaapi.HelmPreparedRelease{ChartArchive: base64.StdEncoding.EncodeToString(archive)}}
	input, err := Prepare(context.Background(), cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Snapshot.TemplateInputs = &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 3, TemplateDigest: "immutable"}
	input.Snapshot.Migration = &sohaapi.TemplateMigrationAssessment{Initialization: true, FromVersion: 0, ToVersion: 3, RequiresApproval: true}
	release := &releasev1.Release{Name: "app", Namespace: "dev", Version: 1, Manifest: input.Prepared.Manifest, Labels: helmDeliveryLabels(input.Snapshot), Info: &releasev1.Info{Status: releasecommon.StatusDeployed}}
	if err := cfg.Releases.Create(release); err != nil {
		t.Fatal(err)
	}
	client.missing = false
	client.uid = "created"
	for _, mode := range []string{"missing", "original", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			candidate := input
			if mode != "missing" {
				candidate.InitializationResources = []sohaapi.ManifestResourceInventory{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "dev", Name: "app", UID: "created"}}
			}
			if mode == "replaced" {
				client.uid = "replacement"
			} else {
				client.uid = "created"
			}
			candidate.Action = sohaapi.Observe
			result, err := Execute(context.Background(), cfg, candidate)
			if (err == nil) != (mode == "original") || result.Ready != (mode == "original") {
				t.Fatalf("observation %s: %+v %v", mode, result, err)
			}
			candidate.Action = sohaapi.Preflight
			prepared, err := Prepare(context.Background(), cfg, candidate)
			if (err == nil) != (mode == "original") {
				t.Fatalf("recovery %s: %v", mode, err)
			}
			if err == nil && (prepared.Snapshot.ExpectedRevision != 0 || prepared.Snapshot.Operation != sohaapi.HelmDeliverySnapshotOperationInstall) {
				t.Fatal("recovery turned initialization into upgrade")
			}
		})
	}
}

func TestHelmInitializationReceiptRejectsMissingReplacedAndDuplicateUID(t *testing.T) {
	original := []sohaapi.ManifestResourceInventory{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "dev", Name: "app", UID: "created"}}
	if err := ValidateInitializationResources(original, original); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "replaced", "empty", "duplicate"} {
		observed, receipt := slices.Clone(original), slices.Clone(original)
		switch mode {
		case "missing":
			receipt = nil
		case "replaced":
			observed[0].UID = "replacement"
		case "empty":
			receipt[0].UID = ""
		case "duplicate":
			receipt = append(receipt, receipt[0])
			observed = append(observed, observed[0])
		}
		if ValidateInitializationResources(observed, receipt) == nil {
			t.Fatalf("accepted %s receipt", mode)
		}
	}
	guard := &initializingHelmClient{created: map[string]types.UID{"/ConfigMap/dev/app": "created"}}
	if err := guard.validateCreated(sohaapi.HelmExecutionTaskResult{Resources: original}); err != nil {
		t.Fatal(err)
	}
	original[0].UID = "replacement"
	if guard.validateCreated(sohaapi.HelmExecutionTaskResult{Resources: original}) == nil {
		t.Fatal("replacement registered as original creation")
	}
}

func TestHelmInitializationPreflightRequiresAbsentResources(t *testing.T) {
	input := sohaapi.HelmExecutionTaskPayload{Snapshot: sohaapi.HelmDeliverySnapshot{Operation: sohaapi.HelmDeliverySnapshotOperationInstall}, Prepared: &sohaapi.HelmPreparedRelease{Manifest: "manifest"}}
	for _, missing := range []bool{false, true} {
		cfg := &action.Configuration{KubeClient: &observedHelmKubeClient{missing: missing}}
		err := requireInitialHelmResourcesAbsent(cfg, input)
		if (err == nil) != missing {
			t.Fatalf("absence=%v: %v", missing, err)
		}
	}
	input.Snapshot.ExpectedRevision = 1
	if requireInitialHelmResourcesAbsent(&action.Configuration{}, input) == nil {
		t.Fatal("existing revision accepted as initialization")
	}
}

func TestHelmInitializationForcesAtomicCreateAndUIDBoundHookCleanup(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "create-race"}[race], func(t *testing.T) {
			uid := ""
			methods := []string{}
			object := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app","namespace":"dev","uid":"created","resourceVersion":"7","annotations":{"helm.sh/hook":"post-install"}}}`
			client := &restfake.RESTClient{GroupVersion: schema.GroupVersion{Version: "v1"}, NegotiatedSerializer: kuberesource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer, Client: restfake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
				methods = append(methods, req.Method)
				code, body := http.StatusOK, object
				switch req.Method {
				case http.MethodGet:
					if uid == "" {
						code, body = http.StatusNotFound, `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404}`
					} else {
						body = strings.ReplaceAll(object, "created", uid)
					}
				case http.MethodPost:
					if race {
						uid = "foreign"
						code, body = http.StatusConflict, `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"AlreadyExists","code":409}`
					} else {
						uid = "created"
					}
				case http.MethodDelete:
					var options metav1.DeleteOptions
					if err := json.NewDecoder(req.Body).Decode(&options); err != nil || options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != "created" || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "7" {
						t.Fatal("hook cleanup lost UID/resourceVersion preconditions", err)
					}
					uid = ""
				default:
					t.Fatalf("initialization issued unsafe HTTP %s", req.Method)
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var desired unstructured.Unstructured
			if err := json.Unmarshal([]byte(object), &desired); err != nil {
				t.Fatal(err)
			}
			desired.SetUID("")
			desired.SetResourceVersion("")
			resources := kube.ResourceList{&kuberesource.Info{Client: client, Namespace: "dev", Name: "app", Object: &desired, Mapping: &meta.RESTMapping{Resource: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Scope: meta.RESTScopeNamespace}}}
			native := &kube.Client{}
			native.SetLogger(slog.NewTextHandler(io.Discard, nil))
			guard := &initializingHelmClient{Interface: native, created: map[string]types.UID{}}
			_, err := guard.Create(resources, kube.ClientCreateOptionServerSideApply(true, true))
			if race && !apierrors.IsAlreadyExists(err) || !race && err != nil {
				t.Fatalf("atomic create race=%v: %v", race, err)
			}
			if !reflect.DeepEqual(methods, []string{http.MethodGet, http.MethodGet, http.MethodPost}) {
				t.Fatalf("native initialization did not use GET/POST only: %v", methods)
			}
			if _, err := guard.Update(nil, resources); err == nil {
				t.Fatal("initialization adopted existing resources through Update")
			}
			_, errs := guard.Delete(resources, metav1.DeletePropagationBackground)
			if (len(errs) == 0) == race {
				t.Fatalf("hook ownership race=%v: %v", race, errs)
			}
			if race && methods[len(methods)-1] != http.MethodGet || !race && methods[len(methods)-1] != http.MethodDelete {
				t.Fatalf("hook deletion ignored ownership: %v", methods)
			}
		})
	}
}
