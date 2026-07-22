package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type fixedRuntimeResources struct {
	snapshot sohaapi.RuntimeResourceSnapshot
}

func (r fixedRuntimeResources) Snapshot() sohaapi.RuntimeResourceSnapshot { return r.snapshot }

func TestRuntimeConfigResourcesReturnsEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewRuntimeConfigHandler(nil, fixedRuntimeResources{snapshot: sohaapi.RuntimeResourceSnapshot{
		UptimeSeconds: 42,
		CPU:           sohaapi.RuntimeCPUUsage{LogicalCores: 8, UsagePercent: 12.5},
	}})
	router := gin.New()
	router.GET("/resources", handler.Resources)

	request := httptest.NewRequest(http.MethodGet, "/resources", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope sohaapi.RuntimeResourceSnapshotEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.UptimeSeconds != 42 || envelope.Data.CPU.LogicalCores != 8 {
		t.Fatalf("unexpected response: %#v", envelope.Data)
	}
}
