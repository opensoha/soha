package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
)

func TestRBACGraphHTTPReadScopeAndAuth(t *testing.T) {
	const (
		namespace      = "team-a"
		rbacAPI        = "rbac.authorization.k8s.io/v1"
		rbacPath       = "/apis/" + rbacAPI
		namespacedRBAC = rbacPath + "/namespaces/" + namespace
		namespacedCore = "/api/v1/namespaces/" + namespace
	)
	role := rbacGraphFixtureObject(rbacAPI, "Role", namespace, "")
	clusterRole := rbacGraphFixtureObject(rbacAPI, "ClusterRole", "", "")
	account := rbacGraphFixtureObject("v1", "ServiceAccount", namespace, "")
	roleBinding := rbacGraphFixtureObject(rbacAPI, "RoleBinding", namespace, `,"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"Role","name":"root"},"subjects":[{"kind":"ServiceAccount","name":"root"}]`)
	clusterRoleBinding := rbacGraphFixtureObject(rbacAPI, "ClusterRoleBinding", "", `,"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"root"},"subjects":[{"kind":"ServiceAccount","name":"root","namespace":"team-a"}]`)
	pod := rbacGraphFixtureObject("v1", "Pod", namespace, `,"spec":{"serviceAccountName":"root"}`)
	listBodies := map[string]string{
		namespacedRBAC + "/roles":           rbacGraphFixtureList(rbacAPI, "Role", role),
		namespacedRBAC + "/rolebindings":    rbacGraphFixtureList(rbacAPI, "RoleBinding", roleBinding),
		rbacPath + "/clusterroles":          rbacGraphFixtureList(rbacAPI, "ClusterRole", clusterRole),
		rbacPath + "/clusterrolebindings":   rbacGraphFixtureList(rbacAPI, "ClusterRoleBinding", clusterRoleBinding),
		rbacPath + "/rolebindings":          rbacGraphFixtureList(rbacAPI, "RoleBinding", roleBinding),
		namespacedCore + "/serviceaccounts": rbacGraphFixtureList("v1", "ServiceAccount", account),
		namespacedCore + "/pods":            rbacGraphFixtureList("v1", "Pod", pod),
		"/api/v1/serviceaccounts":           rbacGraphFixtureList("v1", "ServiceAccount", account),
	}
	for _, root := range []struct {
		kind, namespace, collection, object, relation string
		lists                                         []string
	}{
		{"ServiceAccount", namespace, namespacedCore + "/serviceaccounts", account, "uses-service-account",
			[]string{rbacPath + "/clusterrolebindings", namespacedCore + "/pods", namespacedRBAC + "/rolebindings"}},
		{"Role", namespace, namespacedRBAC + "/roles", role, "binds-role",
			[]string{namespacedRBAC + "/rolebindings"}},
		{"RoleBinding", namespace, namespacedRBAC + "/rolebindings", roleBinding, "binds-subject",
			[]string{rbacPath + "/clusterroles", namespacedRBAC + "/roles", namespacedCore + "/serviceaccounts"}},
		{"ClusterRole", "", rbacPath + "/clusterroles", clusterRole, "binds-role",
			[]string{rbacPath + "/clusterroles", rbacPath + "/clusterrolebindings", rbacPath + "/rolebindings"}},
		{"ClusterRoleBinding", "", rbacPath + "/clusterrolebindings", clusterRoleBinding, "binds-subject",
			[]string{rbacPath + "/clusterroles", "/api/v1/serviceaccounts"}},
	} {
		t.Run(root.kind, func(t *testing.T) {
			expected := map[string]string{root.collection + "/root": root.object}
			for _, path := range root.lists {
				expected[path+"?limit="+strconv.FormatInt(contractruntime.ResourceGraphSourceLimit, 10)] = listBodies[path]
			}
			var reads sync.Map
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				query.Del("timeout") // The Kubernetes transport adds its request deadline.
				key := r.URL.Path
				if encoded := query.Encode(); encoded != "" {
					key += "?" + encoded
				}
				body, ok := expected[key]
				if _, repeated := reads.LoadOrStore(key, true); repeated || !ok || r.Method != http.MethodGet {
					t.Errorf("unexpected Kubernetes request %s %s", r.Method, r.URL.RequestURI())
					http.Error(w, "private-provider-detail", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer upstream.Close()
			router := newGraphPlatformRouter(t, upstream.URL)
			request := func(ns string, auth bool) *httptest.ResponseRecorder {
				target := graphTestPath + "?" + url.Values{"kind": {root.kind}, "name": {"root"}, "namespace": {ns}}.Encode()
				req := httptest.NewRequest(http.MethodGet, target, nil)
				if auth {
					req.Header.Set("Authorization", "Bearer test-token")
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return response
			}
			if response := request(root.namespace, false); response.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated read=%d %s", response.Code, response.Body)
			}
			invalidNS := namespace
			if root.namespace != "" {
				invalidNS = ""
			}
			if response := request(invalidNS, true); response.Code != http.StatusBadRequest {
				t.Fatalf("invalid scope=%d %s", response.Code, response.Body)
			}
			reads.Range(func(key, _ any) bool {
				t.Errorf("unauthenticated or invalid scope reached Kubernetes: %s", key)
				return true
			})

			response := request(root.namespace, true)
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-provider-detail") {
				t.Fatalf("graph=%d %s", response.Code, response.Body)
			}
			for key := range expected {
				if _, found := reads.Load(key); !found {
					t.Errorf("missing Kubernetes read %s", key)
				}
			}
			var envelope struct {
				Data contractresource.ResourceGraph `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			assertRBACGraphTransport(t, envelope.Data, root.kind, root.namespace, root.relation)
		})
	}
}

func rbacGraphFixtureObject(apiVersion, kind, namespace, fields string) string {
	return fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"namespace":%q,"name":"root","uid":%q}%s}`, apiVersion, kind, namespace, kind+"-uid", fields)
}

func rbacGraphFixtureList(apiVersion, kind, item string) string {
	return fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"items":[%s]}`, apiVersion, kind+"List", item)
}

func assertRBACGraphTransport(t *testing.T, graph contractresource.ResourceGraph, kind, namespace, relation string) {
	t.Helper()
	var root contractresource.ResourceRef
	for _, node := range graph.Nodes {
		if node.ID == graph.RootID {
			root = node.Resource
			break
		}
	}
	scope := contractresource.ResourceScopeModeCluster
	if namespace != "" {
		scope = contractresource.ResourceScopeModeNamespace
	}
	if root.Kind != kind || root.Namespace != namespace || root.Name != "root" || root.ClusterID != "cluster-a" || root.ScopeMode != scope || root.UID != kind+"-uid" {
		t.Fatalf("unexpected graph root: %+v", root)
	}
	for _, edge := range graph.Edges {
		if edge.Relation == relation && edge.ResolutionState == "resolved" &&
			(edge.SourceID == graph.RootID || edge.TargetID == graph.RootID) {
			return
		}
	}
	t.Fatalf("missing resolved %q edge for %s: %+v", relation, kind, graph.Edges)
}
