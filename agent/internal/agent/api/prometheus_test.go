package api

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrometheusProxyBoundsAndAuthentication(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer upstream-secret" || r.URL.Query().Get("query") != "up" {
			t.Error("query or upstream authentication lost")
		}
		if r.URL.Path == "/api/v1/query_range" && r.URL.Query().Get("step") != "10" {
			t.Error("range lost")
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
	}))
	defer upstream.Close()
	router := gin.New()
	group := router.Group("/platform")
	group.Use(authMiddleware("agent-secret"))
	registerPrometheusRoutes(group, cfgpkg.PrometheusConfig{BaseURL: upstream.URL, BearerToken: "upstream-secret"})
	for _, tc := range []struct {
		token  string
		query  contractresource.PrometheusQuery
		status int
	}{
		{"agent-secret", contractresource.PrometheusQuery{Endpoint: upstream.URL, Query: "up", Kind: "instant"}, 200},
		{"agent-secret", contractresource.PrometheusQuery{Endpoint: upstream.URL, Query: "up", Kind: "range", Start: 100, End: 200, Step: 10}, 200},
		{"wrong", contractresource.PrometheusQuery{Endpoint: upstream.URL, Query: "up", Kind: "instant"}, 401},
		{"agent-secret", contractresource.PrometheusQuery{Endpoint: "http://attacker", Query: "up", Kind: "instant"}, 409},
		{"agent-secret", contractresource.PrometheusQuery{Endpoint: upstream.URL, Query: "up", Kind: "range", Start: 1, End: 200000, Step: 10}, 400},
	} {
		body, _ := json.Marshal(tc.query)
		req := httptest.NewRequest(http.MethodPost, "/platform/metrics/prometheus/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tc.token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("status=%d want=%d", response.Code, tc.status)
		}
		if strings.Contains(response.Body.String(), "upstream-secret") {
			t.Fatal("token leaked")
		}
	}
	if calls != 2 {
		t.Fatalf("rejected requests reached upstream: %d", calls)
	}
}

func TestPrometheusProxyNeverFollowsRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer upstream.Close()
	router := gin.New()
	registerPrometheusRoutes(router.Group(""), cfgpkg.PrometheusConfig{BaseURL: upstream.URL, BearerToken: "secret"})
	body, _ := json.Marshal(contractresource.PrometheusQuery{Endpoint: upstream.URL, Query: "up", Kind: "instant"})
	req := httptest.NewRequest(http.MethodPost, "/metrics/prometheus/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 502 || redirected {
		t.Fatal("redirect followed")
	}
}
