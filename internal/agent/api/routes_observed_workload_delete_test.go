package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObservedWorkloadDeleteUsesAgentPolicyAndKubernetesPreconditions(t *testing.T) {
	for _, tc := range []struct {
		name, kind, uid       string
		allowed, kubeConflict bool
		status, writes        int32
	}{
		{name: "pod", kind: "Pod", uid: "live-uid", allowed: true, status: 204, writes: 1},
		{name: "deployment", kind: "Deployment", uid: "live-uid", allowed: true, status: 204, writes: 1},
		{name: "same-name-pod", kind: "Pod", uid: "old-uid", allowed: true, status: 409},
		{name: "same-name-deployment", kind: "Deployment", uid: "old-uid", allowed: true, status: 409},
		{name: "replacement-after-get", kind: "Pod", uid: "live-uid", allowed: true, kubeConflict: true, status: 409, writes: 1},
		{name: "empty-uid", kind: "Pod", allowed: true, status: 400},
		{name: "unsupported-kind", kind: "Secret", uid: "live-uid", allowed: true, status: 400},
		{name: "not-allowlisted", kind: "Pod", uid: "live-uid", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					version := "v1"
					if tc.kind == "Deployment" {
						version = "apps/v1"
					}
					fmt.Fprintf(w, `{"apiVersion":%q,"kind":%q,"metadata":{"name":"app","namespace":"team","uid":"live-uid","resourceVersion":"7"}}`, version, tc.kind)
					return
				}
				if r.Method != http.MethodDelete {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				writes.Add(1)
				var options metav1.DeleteOptions
				if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
					t.Error(err)
				} else if p := options.Preconditions; p == nil || p.UID == nil || *p.UID != "live-uid" || p.ResourceVersion == nil || *p.ResourceVersion != "7" {
					t.Errorf("delete options lack observed identity: %+v", p)
				}
				if tc.kubeConflict {
					w.WriteHeader(http.StatusConflict)
					fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Conflict","code":409}`)
					return
				}
				fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Success"}`)
			}))
			defer upstream.Close()
			client, err := k8sagent.New(cfgpkg.KubernetesConfig{ID: "test", KubeconfigData: fmt.Sprintf(`apiVersion: v1
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
`, upstream.URL)})
			if err != nil {
				t.Fatal(err)
			}
			policy := cfgpkg.SecurityConfig{}
			if tc.allowed {
				policy.AllowedActions = []string{actionPlatformResourcesDelete}
			}
			router := gin.New()
			registerObservedWorkloadDeleteRoute(router.Group("/ownership-v2", authMiddleware("token")), client, newActionPolicy(policy, nil, nil))
			req := httptest.NewRequest(http.MethodDelete, "/ownership-v2/resources/observed", strings.NewReader(fmt.Sprintf(`{"namespace":"team","kind":%q,"name":"app","expectedUid":%q}`, tc.kind, tc.uid)))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != int(tc.status) || writes.Load() != tc.writes {
				t.Fatalf("status=%d, writes=%d, body=%q", response.Code, writes.Load(), response.Body.String())
			}
		})
	}
}
