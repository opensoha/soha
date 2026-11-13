package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	cr "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTopologyRPCAuthenticatesValidatesAndReportsDisabledVendor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	platform := router.Group("/api/v1/platform", authAnyMiddleware("token", ""))
	registerTopologySnapshotRoute(platform, nil, newActionPolicy(cfgpkg.SecurityConfig{}, nil, nil))
	cases := []struct {
		body, token string
		status      int
	}{
		{`{"queries":[{"sourceId":"traefik.ingressroutes","namespace":"team"}]}`, "", 401},
		{`{"queries":[{"sourceId":"secrets","namespace":"team"}]}`, "token", 400},
		{`{"queries":[{"sourceId":"traefik.ingressroutes","namespace":"team","name":"route"}]}`, "token", 400},
		{`{"queries":[{"sourceId":"kubernetes.services","namespace":"team","name":"../secret"}]}`, "token", 400},
		{`{"queries":[{"sourceId":"kubernetes.pods","namespace":"../team"}]}`, "token", 400},
		{`{"queries":[{"sourceId":"traefik.ingressroutes","namespace":"team"}]}`, "token", 200},
		{`{"queries":[{"sourceId":"gateway.tcproutes","namespace":"team"}]}`, "token", 200},
		{`{"queries":[{"sourceId":"gateway.tlsroutes","namespace":"team"}]}`, "token", 200},
		{`{"queries":[{"sourceId":"gateway.udproutes","namespace":"team"}]}`, "token", 200},
	}
	for _, test := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/network/topology/snapshot", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
		}
		if test.status == 200 {
			var output struct {
				Data cr.NetworkTopologySnapshot `json:"data"`
			}
			if json.Unmarshal(response.Body.Bytes(), &output) != nil || output.Data.Sources[0].Coverage.State != "denied" || len(output.Data.Sources[0].Objects) != 0 {
				t.Fatal("disabled source invoked Kubernetes or fabricated coverage")
			}
		}
	}
}

func TestTopologyAndMonitoringReadThroughAuthenticatedHTTPAndKubernetesREST(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1":
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "services", Kind: "Service", Namespaced: true}}})
		case "/api/v1/namespaces/team/services/web":
			_ = json.NewEncoder(w).Encode(corev1.Service{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}, ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "team", UID: "web-uid", Annotations: map[string]string{"private": "do-not-transport"}}})
		case "/api/v1/namespaces/team/services":
			_ = json.NewEncoder(w).Encode(corev1.ServiceList{Items: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "team", UID: "web-uid", Annotations: map[string]string{"private": "do-not-transport"}}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}}}}})
		case "/api/v1/nodes":
			_ = json.NewEncoder(w).Encode(corev1.NodeList{Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "worker", UID: "worker-uid"}}}})
		case "/api/v1/pods":
			_ = json.NewEncoder(w).Encode(corev1.PodList{Items: []corev1.Pod{}})
		default:
			t.Errorf("unexpected Kubernetes path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer kube.Close()
	client, err := k8sagent.New(cfgpkg.KubernetesConfig{ID: "c", KubeconfigData: fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: fixture\ncontexts:\n- name: fixture\n  context: {cluster: fixture, user: fixture}\nclusters:\n- name: fixture\n  cluster: {server: %s}\nusers:\n- name: fixture\n  user: {}\n", kube.URL)})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerTopologySnapshotRoute(router.Group("/api/v1/platform", authAnyMiddleware("token", "")), client, newActionPolicy(cfgpkg.SecurityConfig{}, nil, nil))
	server := httptest.NewServer(router)
	defer server.Close()
	for _, test := range []struct {
		path, method, body string
		authorize          bool
		status             int
	}{
		{"/infrastructure/resource-monitoring", http.MethodGet, "", false, 401},
		{"/infrastructure/resource-monitoring", http.MethodGet, "", true, 200},
		{"/network/topology/snapshot", http.MethodPost, `{"queries":[{"sourceId":"kubernetes.services","namespace":"team"}]}`, true, 200},
		{"/network/topology/snapshot", http.MethodPost, `{"queries":[{"sourceId":"kubernetes.services","namespace":"team","name":"web"}]}`, true, 200},
	} {
		request, _ := http.NewRequest(test.method, server.URL+"/api/v1/platform"+test.path, strings.NewReader(test.body))
		if test.authorize {
			request.Header.Set("Authorization", "Bearer token")
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("%s status=%d body=%s", test.path, response.StatusCode, raw)
		}
		if !test.authorize {
			continue
		}
		if strings.Contains(string(raw), "do-not-transport") {
			t.Fatal("full manifest escaped projected topology")
		}
		if test.method == http.MethodGet {
			var payload struct {
				Data cr.ResourceMonitoringInventory `json:"data"`
			}
			if json.Unmarshal(raw, &payload) != nil || len(payload.Data.Nodes) != 1 || payload.Data.Nodes[0].NodeRef.ClusterID != "c" || payload.Data.Nodes[0].NodeRef.UID != "worker-uid" {
				t.Fatalf("inventory did not reach Kubernetes: %s", raw)
			}
		} else {
			var payload struct {
				Data cr.NetworkTopologySnapshot `json:"data"`
			}
			if strings.Contains(test.body, `"name":"web"`) {
				if json.Unmarshal(raw, &payload) != nil || payload.Data.Version != cr.NetworkTopologyNamedRootFeature || len(payload.Data.Sources) != 1 || payload.Data.Sources[0].Coverage.Name != "web" {
					t.Fatalf("named proof lost: %s", raw)
				}
			}
			if json.Unmarshal(raw, &payload) != nil || len(payload.Data.Sources) != 1 || payload.Data.Sources[0].Coverage.State != "available" || len(payload.Data.Sources[0].Objects) != 1 || payload.Data.Sources[0].Objects[0].Resource.UID != "web-uid" {
				t.Fatalf("topology did not reach Kubernetes: %s", raw)
			}
		}
	}
}
