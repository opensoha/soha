package outpostruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	app "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
)

type managementStub struct{ called bool }

func (s *managementStub) View(context.Context, identity.Principal, string) (app.View, error) {
	return app.View{OutpostID: "edge", Owner: "external", Phase: "unknown"}, nil
}
func (s *managementStub) DescribeState(context.Context, domain.State) (app.View, error) {
	return app.View{OutpostID: "edge", Owner: "soha", Phase: "pending"}, nil
}
func (s *managementStub) Bind(context.Context, identity.Principal, string, domain.BindingInput) (domain.State, error) {
	s.called = true
	return domain.State{}, nil
}
func (s *managementStub) Control(context.Context, identity.Principal, string, domain.ControlInput) (domain.State, error) {
	s.called = true
	return domain.State{}, nil
}

func TestRuntimeControlBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, body string
		status     int
		called     bool
	}{
		{"valid", `{"action":"scale","replicas":2,"expectedGeneration":1,"requestId":"request"}`, 202, true},
		{"replica overflow", `{"action":"scale","replicas":4294967297,"expectedGeneration":1,"requestId":"request"}`, 400, false},
		{"arbitrary target", `{"action":"start","expectedGeneration":1,"requestId":"request","namespace":"other"}`, 400, false},
		{"multiple documents", `{} {}`, 400, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &managementStub{}
			router := gin.New()
			router.POST("/runtime/control", New(service, nil).Control)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/runtime/control", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code != test.status || service.called != test.called {
				t.Fatalf("status=%d called=%v", response.Code, service.called)
			}
		})
	}
}

func TestExecutorRejectsAmbiguousCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/status", New(nil, nil).Status)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/status?taskId=task", nil)
	request.Header.Add("Authorization", "Bearer first")
	request.Header.Add("Authorization", "Bearer second")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("accepted ambiguous credential: %d", response.Code)
	}
}

func TestRuntimeViewPreservesUnknownObservations(t *testing.T) {
	encoded, err := json.Marshal(runtimeDTO(app.View{OutpostID: "edge", Owner: "external", Phase: "unknown"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"actualReplicas", "readyReplicas", "observedGeneration", "ownerEpoch", "executorId"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("unexpected observation/internal field %s", field)
		}
	}
}
