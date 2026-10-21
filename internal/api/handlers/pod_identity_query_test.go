package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPodSessionRejectsAmbiguousUIDBeforeBackendOrUpgrade(t *testing.T) {
	for _, query := range []string{"expectedUid=", "expectedUid=%20", "expectedUid=%XX", "expectedUid=&expectedUid=old", "expectedUid=old&expectedUid=other"} {
		for name, handler := range map[string]gin.HandlerFunc{"snapshot": (&podResourceHandler{}).GetPodLogs, "logs": (&podStreamResourceHandler{}).StreamPodLogs, "terminal": (&podStreamResourceHandler{}).StreamPodTerminal} {
			t.Run(name+"/"+query, func(t *testing.T) {
				router := gin.New()
				router.GET("/session", handler)
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, "/session?"+query, nil)
				router.ServeHTTP(response, request)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
				}
			})
		}
	}
}
