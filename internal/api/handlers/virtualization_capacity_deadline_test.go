package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type slowCapacityPlanning struct {
	VirtualizationPlanningService
	t                *testing.T
	expectedDeadline *time.Time
}

func (s slowCapacityPlanning) CheckCapacity(ctx context.Context, _ domainidentity.Principal, _ sohaapi.VirtualizationCapacityInput) (sohaapi.VirtualizationCapacityResult, error) {
	return sohaapi.VirtualizationCapacityResult{}, waitForCapacityFixture(s.t, ctx, s.expectedDeadline)
}

type slowCapacityGateway struct {
	AIGatewayCapabilityService
	t                *testing.T
	expectedDeadline *time.Time
}

func (s slowCapacityGateway) InvokeTool(ctx context.Context, _ domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	return domainaigateway.ToolInvocationResult{ToolName: input.ToolName, Output: map[string]any{"capacity": "fixture"}}, waitForCapacityFixture(s.t, ctx, s.expectedDeadline)
}

func waitForCapacityFixture(t *testing.T, ctx context.Context, expectedDeadline *time.Time) error {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Minute {
		t.Error("capacity read must have a bounded deadline")
	}
	if expectedDeadline != nil && !deadline.Equal(*expectedDeadline) {
		t.Error("capacity read extended the caller deadline")
	}
	select {
	case <-time.After(100 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestCapacityReadPreservesCallerCancellation(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		name := "workbench"
		if gateway {
			name = "gateway"
		}
		t.Run(name, func(t *testing.T) {
			deadline := time.Now().Add(20 * time.Millisecond)
			caller, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/capacity", strings.NewReader(`{}`)).WithContext(caller)
			if gateway {
				c.Params = gin.Params{{Key: "toolName", Value: "virtualization.capacity.check"}}
				(&aiGatewayCapabilityHandler{service: slowCapacityGateway{t: t, expectedDeadline: &deadline}}).InvokeTool(c)
			} else {
				(&VirtualizationHandler{planning: slowCapacityPlanning{t: t, expectedDeadline: &deadline}}).CheckCapacity(c)
			}
			if recorder.Code == http.StatusOK || caller.Err() != context.DeadlineExceeded {
				t.Fatalf("capacity ignored caller cancellation: status=%d, error=%v", recorder.Code, caller.Err())
			}
		})
	}
}

type unchangedDeadlineGateway struct {
	AIGatewayCapabilityService
	t               *testing.T
	expectedContext context.Context
}

func (s unchangedDeadlineGateway) InvokeTool(ctx context.Context, _ domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	if ctx != s.expectedContext || input.ToolName != "virtualization.vm.get" {
		s.t.Error("non-capacity tool invocation changed")
	}
	return domainaigateway.ToolInvocationResult{}, nil
}

func TestCapacityDeadlineDoesNotChangeOtherTools(t *testing.T) {
	for _, routeTool := range []string{"virtualization.vm.get", "virtualization.capacity.check"} {
		t.Run(routeTool, func(t *testing.T) {
			deadline := time.Now().Add(20 * time.Millisecond)
			recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), deadline: deadline}
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/tools/"+routeTool+"/invoke", strings.NewReader(`{"toolName":"virtualization.vm.get"}`))
			c.Params = gin.Params{{Key: "toolName", Value: routeTool}}
			service := unchangedDeadlineGateway{t: t, expectedContext: c.Request.Context()}
			(&aiGatewayCapabilityHandler{service: service}).InvokeTool(c)
			if recorder.Code != http.StatusOK || !recorder.deadline.Equal(deadline) {
				t.Fatal("non-capacity tool changed the ordinary write deadline")
			}
		})
	}
}

func TestCapacityReadRespondsAfterOrdinaryWriteTimeout(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		name := "workbench"
		if gateway {
			name = "gateway"
		}
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			path, input := "/capacity", `{}`
			if gateway {
				router.POST("/tools/:toolName/invoke", (&aiGatewayCapabilityHandler{service: slowCapacityGateway{t: t}}).InvokeTool)
				path, input = "/tools/virtualization.capacity.check/invoke", `{"input":{}}`
			} else {
				router.POST(path, (&VirtualizationHandler{planning: slowCapacityPlanning{t: t}}).CheckCapacity)
			}
			server := httptest.NewUnstartedServer(router)
			server.Config.WriteTimeout = 20 * time.Millisecond
			server.Start()
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second
			resp, err := client.Post(server.URL+path, "application/json", strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			var body struct {
				Data map[string]any `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || body.Data == nil {
				t.Fatalf("capacity response missing: status=%d", resp.StatusCode)
			}
		})
	}
}
