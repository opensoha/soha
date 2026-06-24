package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestWriteErrorPreservesClientFacingMessageForNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	writeError(ctx, apperrors.ErrNotFound)

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
	if payload.Error.Message != apperrors.ErrNotFound.Error() {
		t.Fatalf("message = %q, want %q", payload.Error.Message, apperrors.ErrNotFound.Error())
	}
}
