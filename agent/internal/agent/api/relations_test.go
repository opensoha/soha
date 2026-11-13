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
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	cr "github.com/opensoha/soha/contracts/resource"
)

func TestGenericRelationRPCChecksAuthActionsAndMetadataProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var reads atomic.Int64
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reads.Add(1)
		if r.URL.Path == "/apis/unknown.test/v1" {
			_, _ = w.Write([]byte(`{"resources":[{"name":"widgets","kind":"UnknownWidget","namespaced":true}]}`))
			return
		}
		if r.URL.Path != "/apis/unknown.test/v1/namespaces/team/widgets" || r.URL.Query().Get("limit") != "200" {
			t.Errorf("unexpected metadata request %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"child","namespace":"team","uid":"child-uid","ownerReferences":[{"apiVersion":"unknown.test/v1","kind":"UnknownWidget","name":"root","uid":"root-uid"}],"annotations":{"password":"synthetic-private"}},"spec":{"password":"synthetic-private"}}]}`))
	}))
	defer kube.Close()
	client, err := k8sagent.New(cfgpkg.KubernetesConfig{ID: "cluster", KubeconfigData: fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: fixture\ncontexts:\n- name: fixture\n  context: {cluster: fixture, user: fixture}\nclusters:\n- name: fixture\n  cluster: {server: %s}\nusers:\n- name: fixture\n  user: {}\n", kube.URL)})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"queries":[{"id":"unknown","source":{"apiVersion":"unknown.test/v1","resource":"widgets","kind":"UnknownWidget","namespace":"team"}}]}`
	for _, test := range []struct {
		name    string
		token   bool
		actions []string
		status  int
		state   string
	}{
		{"auth", false, []string{actionPlatformCustomResourcesList}, 401, ""},
		{"action", true, nil, 403, ""},

		{"unknown operator", true, []string{actionPlatformCustomResourcesList}, 200, "available"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			registerRelationSnapshotRoute(router.Group("/api/v1/platform", authAnyMiddleware("token", "")), client, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: test.actions}, nil, nil))
			before := reads.Load()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/resources/relations/snapshot", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			if test.token {
				request.Header.Set("Authorization", "Bearer token")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if test.state != "" {
				var out struct {
					Data cr.RelationSnapshot `json:"data"`
				}
				if json.Unmarshal(response.Body.Bytes(), &out) != nil || out.Data.Sources[0].Coverage.State != test.state {
					t.Fatal("incorrect coverage")
				}
			}
			if test.state != "available" && reads.Load() != before {
				t.Fatal("denied request reached Kubernetes")
			}
			if strings.Contains(response.Body.String(), "synthetic-private") {
				t.Fatal("configuration leaked through metadata")
			}
		})
	}
	t.Run("bounded request includes source query overhead", func(t *testing.T) {
		router := gin.New()
		registerRelationSnapshotRoute(router.Group("/api/v1/platform", authAnyMiddleware("token", "")), client, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformCustomResourcesList}}, nil, nil))
		for _, test := range []struct{ padding, status int }{{17 << 10, 200}, {33 << 10, 400}} {
			before := reads.Load()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/resources/relations/snapshot", strings.NewReader(body+strings.Repeat(" ", test.padding)))
			request.Header.Set("Authorization", "Bearer token")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("padding %d: status %d", test.padding, response.Code)
			}
			if test.status == 400 && reads.Load() != before {
				t.Fatal("oversized request reached Kubernetes")
			}
		}
	})

}
