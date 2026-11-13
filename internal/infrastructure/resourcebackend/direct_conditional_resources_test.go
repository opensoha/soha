package resourcebackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	contractresource "github.com/opensoha/soha/contracts/resource"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDirectConditionalPreflightDoesNotExposeProviderConflictValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "app", "namespace": "test", "uid": "reviewed", "resourceVersion": "1"}, "data": map[string]any{"key": "old"}})
			return
		}
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Code: 409, Reason: metav1.StatusReasonConflict, Message: "SENSITIVE_PROVIDER_VALUE"})
	}))
	defer server.Close()
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	direct := NewDirect(NewClusters(k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})), nil)
	analysis, err := direct.DryRunResourceYAMLConditional(t.Context(), "test", "test", "ConfigMap", "app", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  key: new\n", contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed", ExpectedResourceVersion: "1"})
	raw, _ := json.Marshal(analysis)
	if err != nil || len(analysis.Conflicts) == 0 || strings.Contains(string(raw), "SENSITIVE_PROVIDER_VALUE") {
		t.Fatalf("conditional conflict projection: %s %v", raw, err)
	}
}

func TestConditionalResourceErrorsDoNotPersistManifestValues(t *testing.T) {
	for _, input := range []error{
		apierrors.NewBadRequest("SENSITIVE_MANIFEST_VALUE"),
		apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, "tls", fmt.Errorf("SENSITIVE_MANIFEST_VALUE")),
		fmt.Errorf("admission rejected SENSITIVE_MANIFEST_VALUE"),
	} {
		err := conditionalResourceError(input)
		if err == nil || strings.Contains(err.Error(), "SENSITIVE_MANIFEST_VALUE") {
			t.Fatalf("raw admission failure reached audit: %v", err)
		}
	}
}

func TestDirectConditionalMutationsPreserveReviewedIdentity(t *testing.T) {
	for _, test := range []struct {
		name         string
		uid, version string
		owned        bool
		wantError    bool
	}{
		{"matching", "uid-app", "7", false, false},
		{"replaced UID", "uid-old", "7", false, true},
		{"changed version", "uid-app", "6", false, true},
		{"missing guard", "", "", false, true},
		{"foreign owner", "uid-app", "7", true, true},
	} {
		for _, action := range []string{"apply", "preflight", "delete"} {
			t.Run(test.name+"/"+action, func(t *testing.T) {
				var writes atomic.Int32
				direct := directOwnershipFixture(t, test.owned, &writes)
				guard := contractresource.ResourceMutationPreconditions{ExpectedUID: test.uid, ExpectedResourceVersion: test.version}
				content := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: demo\ndata:\n  key: new\n"
				var err error
				switch action {
				case "apply":
					_, err = direct.ApplyResourceYAMLConditional(t.Context(), "test", "demo", "ConfigMap", "app", content, guard)
				case "preflight":
					_, err = direct.DryRunResourceYAMLConditional(t.Context(), "test", "demo", "ConfigMap", "app", content, guard)
				case "delete":
					err = direct.DeleteResourceConditional(t.Context(), "test", "demo", "ConfigMap", "app", guard)
				}
				if (err != nil) != test.wantError {
					t.Fatalf("error=%v", err)
				}
				if test.wantError && writes.Load() != 0 {
					t.Fatalf("rejected request sent %d writes", writes.Load())
				}
			})
		}
	}
}

func TestDirectConditionalEditRejectsStaleYAMLAndScope(t *testing.T) {
	for _, content := range []string{
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: app\n",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  uid: replaced\n",
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  resourceVersion: '6'\n",
	} {
		var writes atomic.Int32
		direct := directOwnershipFixture(t, false, &writes)
		_, err := direct.ApplyResourceYAMLConditional(context.Background(), "test", "demo", "ConfigMap", "app", content, contractresource.ResourceMutationPreconditions{ExpectedUID: "uid-app", ExpectedResourceVersion: "7"})
		if !errors.Is(err, apperrors.ErrInvalidArgument) || writes.Load() != 0 {
			t.Fatalf("error=%v writes=%d", err, writes.Load())
		}
	}
}
