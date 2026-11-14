package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	contractresource "github.com/opensoha/soha/contracts/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

func TestCustomResourceGraphPageHTTP(t *testing.T) {
	const (
		route       = "/api/v1/platform/extensions/custom-resources/graph-page"
		kubeAPI     = "/apis/example.test/v1"
		bearerToken = "test-token"
	)
	var providerReads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerReads.Add(1)
		query := r.URL.Query()
		query.Del("timeout") // The Kubernetes transport adds its request deadline.
		if r.Method != http.MethodGet || query.Encode() != "limit="+strconv.Itoa(contractruntime.ResourceGraphMetadataEnrichmentLimit) {
			t.Errorf("unexpected provider read: %s %s", r.Method, r.URL.RequestURI())
		}
		const metadataAccept = "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1"
		if r.Header.Get("Accept") != metadataAccept {
			t.Errorf("graph page negotiated other than metadata: %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case kubeAPI + "/namespaces/team-blue/widgets":
			_, _ = io.WriteString(w, `{
				"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList",
				"metadata":{"continue":"must-not-read-next-page"},
				"items":[{"metadata":{"name":"child","namespace":"team-blue","uid":"child-uid",
					"annotations":{"example.test/config":"private-annotation"},
					"ownerReferences":[{"apiVersion":"example.test/v1","kind":"Widget","name":"parent","uid":"parent-uid"}]}}]
			}`)
		case kubeAPI + "/widgets":
			_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","items":[]}`)
		case kubeAPI + "/namespaces/team-denied/widgets":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"private-provider-detail"}`)
		case kubeAPI + "/namespaces/team-oversized-error/widgets":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, strings.Repeat("x", 1<<20+1))
		case kubeAPI + "/namespaces/team-full/widgets":
			_, _ = io.WriteString(w, `{"apiVersion":"example.test/v1","kind":"WidgetList","items":[{"spec":{"field":"private-full-object"}}]}`)
		default:
			t.Errorf("unexpected provider scope: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client, err := k8sagent.New(cfgpkg.KubernetesConfig{ID: "cluster-a", KubeconfigData: fmt.Sprintf(`{
		"apiVersion":"v1","kind":"Config","current-context":"fixture",
		"clusters":[{"name":"fixture","cluster":{"server":%q}}],
		"contexts":[{"name":"fixture","context":{"cluster":"fixture","user":"fixture"}}],
		"users":[{"name":"fixture","user":{}}]
	}`, upstream.URL)})
	if err != nil {
		t.Fatal(err)
	}
	buildRouter := func(token string, actions ...string) *gin.Engine {
		cfg := cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: token}}
		router := gin.New()
		policy := newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: actions}, nil, nil)
		registerPlatformRoutes(router, cfg, client, policy)
		return router
	}
	allowed := buildRouter(bearerToken, actionPlatformCustomResourcesList)
	denied := buildRouter(bearerToken, actionPlatformCustomResourcesApply)
	noToken := buildRouter("", actionPlatformCustomResourcesList)
	namespaced := customGraphPageTestBody("team-blue", true)
	for _, test := range []struct {
		name, path, body string
		router           *gin.Engine
		bearer           string
		want             int
	}{
		{"no bearer", route, namespaced, allowed, "", http.StatusUnauthorized},
		{"wrong bearer", route, namespaced, allowed, "wrong-token", http.StatusUnauthorized},
		{"no configured bearer", route, namespaced, noToken, "", http.StatusUnauthorized},
		{"list action denied", route, namespaced, denied, bearerToken, http.StatusForbidden},
		{"no ownership alias", "/api/v1/platform/ownership-v2/extensions/custom-resources/graph-page", namespaced, allowed, bearerToken, http.StatusNotFound},
		{"missing definition", route, `{"namespace":"team-blue"}`, allowed, bearerToken, http.StatusBadRequest},
		{"oversized body", route, namespaced + strings.Repeat(" ", 32<<10), allowed, bearerToken, http.StatusBadRequest},
		{"multiple documents", route, namespaced + " {}", allowed, bearerToken, http.StatusBadRequest},
		{"invalid plural", route, strings.Replace(namespaced, `"resource":"widgets"`, `"resource":"../widgets"`, 1), allowed, bearerToken, http.StatusBadRequest},
		{"cluster definition with namespace", route, customGraphPageTestBody("team-blue", false), allowed, bearerToken, http.StatusBadRequest},
		{"invalid namespace", route, customGraphPageTestBody("../team-blue", true), allowed, bearerToken, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestCustomGraphPage(test.router, test.path, test.bearer, test.body)
			if response.Code != test.want || providerReads.Load() != 0 {
				t.Fatalf("response=%d %s, provider reads=%d", response.Code, response.Body, providerReads.Load())
			}
		})
	}

	response := requestCustomGraphPage(allowed, route, bearerToken, namespaced)
	page := decodeCustomGraphPage(t, response)
	if providerReads.Load() != 1 || !page.Truncated || len(page.Items) != 1 {
		t.Fatalf("provider reads=%d, page=%+v", providerReads.Load(), page)
	}
	item := string(page.Items[0])
	for _, expected := range []string{`"apiVersion":"example.test/v1"`, `"kind":"Widget"`, `"namespace":"team-blue"`, `"uid":"child-uid"`, `"uid":"parent-uid"`} {
		if !strings.Contains(item, expected) {
			t.Errorf("projected item missing %s: %s", expected, item)
		}
	}
	if strings.Contains(item, "private-annotation") || strings.Contains(response.Body.String(), "must-not-read-next-page") {
		t.Fatal("private provider metadata appeared in the graph page")
	}

	page = decodeCustomGraphPage(t, requestCustomGraphPage(allowed, route, bearerToken, customGraphPageTestBody("", false)))
	if providerReads.Load() != 2 || page.Truncated || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("provider reads=%d, empty cluster page=%+v", providerReads.Load(), page)
	}
	response = requestCustomGraphPage(allowed, route, bearerToken, customGraphPageTestBody("team-denied", true))
	if providerReads.Load() != 3 || response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private-provider-detail") {
		t.Fatalf("provider reads=%d, denied response=%d %s", providerReads.Load(), response.Code, response.Body)
	}
	response = requestCustomGraphPage(allowed, route, bearerToken, customGraphPageTestBody("team-full", true))
	if providerReads.Load() != 4 || response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "private-full-object") {
		t.Fatalf("provider reads=%d, non-metadata response=%d %s", providerReads.Load(), response.Code, response.Body)
	}
	response = requestCustomGraphPage(allowed, route, bearerToken, customGraphPageTestBody("team-oversized-error", true))
	if providerReads.Load() != 5 || response.Code != http.StatusBadGateway {
		t.Fatalf("provider reads=%d, oversized error response=%d %s", providerReads.Load(), response.Code, response.Body)
	}
}

func TestCustomResourceGraphObjectHTTP(t *testing.T) {
	const route = "/api/v1/platform" + contractresource.CustomResourceGraphObjectPath
	const bearerToken = "test-token"
	var providerReads, redirects atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			redirects.Add(1)
			return
		}
		providerReads.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1" {
			t.Errorf("unexpected provider read: %s %s accept=%q", r.Method, r.URL.Path, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/example.test/v1/namespaces/team-blue/widgets/child":
			_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"child","namespace":"team-blue","uid":"child-uid","resourceVersion":"42","annotations":{"config":"private-annotation"},"ownerReferences":[{"apiVersion":"example.test/v1","kind":"Widget","name":"parent","uid":"parent-uid"}]}}`)
		case "/apis/example.test/v1/widgets/child":
			_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"child","uid":"cluster-child-uid"}}`)
		case "/apis/example.test/v1/namespaces/team-blue/widgets/forbidden":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"private-provider-detail"}`)
		case "/apis/example.test/v1/namespaces/team-blue/widgets/full":
			_, _ = io.WriteString(w, `{"apiVersion":"example.test/v1","kind":"Widget","metadata":{"name":"full"},"spec":{"field":"private-full-object"}}`)
		case "/apis/example.test/v1/namespaces/team-blue/widgets/wrong":
			_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"different","namespace":"team-blue","uid":"uid"}}`)
		case "/apis/example.test/v1/namespaces/team-blue/widgets/oversized":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, strings.Repeat("x", 1<<20+1))
		case "/apis/example.test/v1/namespaces/team-blue/widgets/redirect":
			http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
		default:
			t.Errorf("unexpected provider scope: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := k8sagent.New(cfgpkg.KubernetesConfig{ID: "cluster-a", KubeconfigData: fmt.Sprintf(`{
		"apiVersion":"v1","kind":"Config","current-context":"fixture",
		"clusters":[{"name":"fixture","cluster":{"server":%q}}],
		"contexts":[{"name":"fixture","context":{"cluster":"fixture","user":"fixture"}}],
		"users":[{"name":"fixture","user":{}}]
	}`, upstream.URL)})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerPlatformRoutes(router, cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: bearerToken}}, client, newActionPolicy(cfgpkg.SecurityConfig{}, nil, nil))
	for _, tc := range []struct {
		name, namespace, objectName, bearer string
		namespaced, upstream                bool
		status                              int
	}{
		{"unauthenticated", "team-blue", "child", "", true, false, http.StatusUnauthorized},
		{"invalid name", "team-blue", "../child", bearerToken, true, false, http.StatusBadRequest},
		{"missing namespace", "", "child", bearerToken, true, false, http.StatusBadRequest},
		{"unexpected namespace", "team-blue", "child", bearerToken, false, false, http.StatusBadRequest},
		{"namespaced", "team-blue", "child", bearerToken, true, true, http.StatusOK},
		{"cluster scoped", "", "child", bearerToken, false, true, http.StatusOK},
		{"Kubernetes RBAC denied", "team-blue", "forbidden", bearerToken, true, true, http.StatusForbidden},
		{"full object rejected", "team-blue", "full", bearerToken, true, true, http.StatusBadGateway},
		{"wrong identity rejected", "team-blue", "wrong", bearerToken, true, true, http.StatusBadGateway},
		{"oversized error rejected", "team-blue", "oversized", bearerToken, true, true, http.StatusBadGateway},
		{"redirect rejected", "team-blue", "redirect", bearerToken, true, true, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := providerReads.Load()
			body := strings.TrimSuffix(customGraphPageTestBody(tc.namespace, tc.namespaced), "}") + fmt.Sprintf(`,"name":%q}`, tc.objectName)
			response := requestCustomGraphPage(router, route, tc.bearer, body)
			want := int32(0)
			if tc.upstream {
				want = 1
			}
			if response.Code != tc.status || providerReads.Load()-before != want || redirects.Load() != 0 || strings.Contains(response.Body.String(), "private-") {
				t.Fatalf("status=%d body=%s provider reads=%d redirects=%d", response.Code, response.Body, providerReads.Load()-before, redirects.Load())
			}
			if tc.status == http.StatusOK {
				uid := "cluster-child-uid"
				if tc.namespaced {
					uid = "child-uid"
				}
				for _, expected := range []string{`"apiVersion":"example.test/v1"`, `"kind":"Widget"`, `"uid":"` + uid + `"`} {
					if !strings.Contains(response.Body.String(), expected) {
						t.Errorf("missing projected identity %s: %s", expected, response.Body)
					}
				}
			}
		})
	}
}

func customGraphPageTestBody(namespace string, namespaced bool) string {
	return fmt.Sprintf(`{"namespace":%q,"definition":{"crdName":"widgets.example.test","group":"example.test","version":"v1","resource":"widgets","kind":"Widget","namespaced":%t}}`, namespace, namespaced)
}

func requestCustomGraphPage(router *gin.Engine, route, bearer, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func decodeCustomGraphPage(t *testing.T, response *httptest.ResponseRecorder) contractresource.CustomResourceGraphPage {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("graph page=%d %s", response.Code, response.Body)
	}
	var envelope struct {
		Data contractresource.CustomResourceGraphPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
