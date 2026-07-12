package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestWriteErrorRedactsInternalServerErrorMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	writeError(ctx, errors.New("db connection refused"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", payload.Error.Code)
	}
	if payload.Error.Message != "internal server error" {
		t.Fatalf("message = %q, want internal server error", payload.Error.Message)
	}
}

func TestWriteErrorUsesStableClientFacingMessageAndRecordsCause(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	cause := fmt.Errorf("load tenant database path: %w", apperrors.ErrNotFound)

	writeError(ctx, cause)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", payload.Error.Code)
	}
	if payload.Error.Message != "resource not found" {
		t.Fatalf("message = %q, want resource not found", payload.Error.Message)
	}
	if len(ctx.Errors) != 1 || !errors.Is(ctx.Errors[0].Err, apperrors.ErrNotFound) {
		t.Fatalf("recorded errors = %#v, want wrapped not-found cause", ctx.Errors)
	}
	if strings.Contains(recorder.Body.String(), "tenant database") {
		t.Fatal("public response contains internal error details")
	}
}
