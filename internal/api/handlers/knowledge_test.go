package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestWriteKnowledgeErrorMapsDomainErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "forbidden", err: domainknowledge.ErrAccessDenied, status: http.StatusForbidden},
		{name: "base not found", err: domainknowledge.ErrBaseNotFound, status: http.StatusNotFound},
		{name: "source not found", err: domainknowledge.ErrSourceNotFound, status: http.StatusNotFound},
		{name: "invalid input", err: domainknowledge.ErrInvalidInput, status: http.StatusBadRequest},
		{name: "candidate limit", err: domainknowledge.ErrRetrievalExhausted, status: http.StatusBadRequest},
		{name: "source unavailable", err: domainknowledge.ErrSourceUnavailable, status: http.StatusServiceUnavailable},
		{name: "ingestion not found", err: domainknowledge.ErrIngestionNotFound, status: http.StatusNotFound},
		{name: "ingestion conflict", err: domainknowledge.ErrIngestionConflict, status: http.StatusConflict},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			writeKnowledgeError(context, fmt.Errorf("load knowledge: %w", test.err))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
