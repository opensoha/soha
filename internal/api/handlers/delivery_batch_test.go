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
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type slowBatchService struct {
	DeliveryBatchService
	t *testing.T
}

func (s slowBatchService) CreateDeliveryBatch(ctx context.Context, _ domainidentity.Principal, _ domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Minute {
		s.t.Error("batch preparation has no bounded deadline")
	}
	select {
	case <-time.After(100 * time.Millisecond):
		return domainworkflow.DeliveryBatch{ID: "batch"}, nil
	case <-ctx.Done():
		return domainworkflow.DeliveryBatch{}, ctx.Err()
	}
}

func TestCreateBatchRespondsAfterOrdinaryWriteTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/delivery-batches", NewDeliveryBatchHandler(slowBatchService{t: t}).CreateBatch)
	server := httptest.NewUnstartedServer(router)
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	response, err := client.Post(server.URL+"/delivery-batches", "application/json", strings.NewReader(`{"idempotencyKey":"batch"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var body struct {
		Data domainworkflow.DeliveryBatch `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || body.Data.ID != "batch" {
		t.Fatalf("response: status %d, batch %q", response.StatusCode, body.Data.ID)
	}
}
