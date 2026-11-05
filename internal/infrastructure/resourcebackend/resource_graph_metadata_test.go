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

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConfigurationGraphDirectMetadataHTTP(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int32
		ordinary bool
	}{
		{"metadata success", http.StatusOK, false},
		{"forbidden", http.StatusForbidden, false},
		{"unsupported metadata", http.StatusNotAcceptable, false},
		{"ordinary Secret rejected", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var namedReads, unsafeReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/secrets") {
					if r.URL.Path != "/api/v1/namespaces/team-a/secrets/credentials" || r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1" {
						unsafeReads.Add(1)
					}
					namedReads.Add(1)
					writeConfigurationSecretMetadataResponse(w, tc.status, tc.ordinary)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: metav1.StatusReasonForbidden, Code: 403})
			}))
			defer server.Close()
			manager := k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "cluster-a", KubeconfigData: fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, server.URL)}})
			direct := NewDirect(NewClusters(manager), nil)
			graph, err := direct.GetResourceGraph(context.Background(), "cluster-a", "team-a", "Secret", "credentials")
			switch {
			case tc.ordinary:
				if err == nil || strings.Contains(err.Error(), "must-not-read") {
					t.Fatalf("ordinary Secret accepted or leaked: err=%v", err)
				}
			case tc.status == http.StatusForbidden:
				if !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("forbidden mapping lost: %v", err)
				}
			case tc.status == http.StatusNotAcceptable:
				if !apierrors.IsNotAcceptable(err) {
					t.Fatalf("metadata refusal lost: %v", err)
				}
			default:
				if err != nil || len(graph.Nodes) != 1 || graph.Nodes[0].Resource.UID != "secret-uid" {
					t.Fatalf("direct metadata graph=%+v err=%v", graph, err)
				}
			}
			if namedReads.Load() != 1 || unsafeReads.Load() != 0 {
				t.Fatalf("unsafe or repeated Secret read: named=%d unsafe=%d", namedReads.Load(), unsafeReads.Load())
			}
		})
	}
}

func writeConfigurationSecretMetadataResponse(w http.ResponseWriter, status int32, ordinary bool) {
	w.WriteHeader(int(status))
	if status == http.StatusOK {
		if ordinary {
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"credentials","namespace":"team-a"},"data":{"password":"must-not-read"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "PartialObjectMetadata"}, ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "team-a", UID: "secret-uid"}})
		return
	}
	reason := metav1.StatusReasonForbidden
	if status == http.StatusNotAcceptable {
		reason = metav1.StatusReasonNotAcceptable
	}
	_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: reason, Code: status})
}
