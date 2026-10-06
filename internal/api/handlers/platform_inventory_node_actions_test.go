package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type nodeActionEditorStub struct {
	NodeEditor
	unschedulable bool
	drainInput    domainresource.NodeDrainInput
}

func (s *nodeActionEditorStub) SetNodeUnschedulable(_ context.Context, _ domainidentity.Principal, _, _ string, value bool) error {
	s.unschedulable = value
	return nil
}

func (s *nodeActionEditorStub) DrainNode(_ context.Context, _ domainidentity.Principal, _, _ string, input domainresource.NodeDrainInput) error {
	s.drainInput = input
	return nil
}

type drainDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (r *drainDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadline = deadline
	return nil
}

func TestNodeActionHandlersBindRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &nodeActionEditorStub{}
	handler := &nodeResourceHandler{editor: stub}
	router := gin.New()
	router.PUT("/clusters/:clusterID/nodes/:nodeName/schedulability", handler.SetNodeSchedulability)
	router.POST("/clusters/:clusterID/nodes/:nodeName/drain", handler.DrainNode)

	request := httptest.NewRequest(http.MethodPut, "/clusters/cluster-a/nodes/node-a/schedulability", strings.NewReader(`{"unschedulable":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !stub.unschedulable {
		t.Fatalf("schedulability response=%d value=%t", response.Code, stub.unschedulable)
	}

	request = httptest.NewRequest(http.MethodPost, "/clusters/cluster-a/nodes/node-a/drain", strings.NewReader(`{"force":true,"deleteEmptyDirData":true,"timeoutSeconds":30}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	deadlineResponse := &drainDeadlineRecorder{ResponseRecorder: response}
	router.ServeHTTP(deadlineResponse, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("drain response=%d body=%s", response.Code, response.Body.String())
	}
	if time.Until(deadlineResponse.deadline) < 40*time.Second {
		t.Fatal("drain write deadline was not extended")
	}
	if !stub.drainInput.Force || !stub.drainInput.DeleteEmptyDirData || stub.drainInput.TimeoutSeconds != 30 {
		t.Fatalf("drain input=%#v", stub.drainInput)
	}
}
