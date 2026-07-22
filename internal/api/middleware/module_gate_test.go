package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

type mutableModuleState struct {
	mu      sync.RWMutex
	enabled bool
}

func (s *mutableModuleState) ModuleEnabled(string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *mutableModuleState) set(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

func TestRequireModuleReadsCurrentStatePerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	state := &mutableModuleState{enabled: true}
	router := gin.New()
	router.GET("/feature", RequireModule(state, "ai"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	assertStatus := func(want int) {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/feature", nil)
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("status = %d, want %d", response.Code, want)
		}
	}

	assertStatus(http.StatusNoContent)
	state.set(false)
	assertStatus(http.StatusNotFound)
	state.set(true)
	assertStatus(http.StatusNoContent)
}
