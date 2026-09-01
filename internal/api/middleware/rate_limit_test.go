package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestBoundedRateLimiterLimitsPerIdentifierAndResets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	limiter := NewBoundedRateLimiter(2)
	limiter.now = func() time.Time { return now }
	router := gin.New()
	router.GET("/token", limiter.Middleware("oidc-token", 1, time.Minute, func(c *gin.Context) string {
		return c.Query("client_id")
	}), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(target string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		return recorder
	}
	if got := request("/token?client_id=a"); got.Code != http.StatusNoContent {
		t.Fatalf("first status = %d", got.Code)
	}
	if got := request("/token?client_id=a"); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited response = %d Retry-After=%q", got.Code, got.Header().Get("Retry-After"))
	}
	if got := request("/token?client_id=b"); got.Code != http.StatusNoContent {
		t.Fatalf("other identifier status = %d", got.Code)
	}
	now = now.Add(time.Minute)
	if got := request("/token?client_id=a"); got.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d", got.Code)
	}
}

func TestBoundedRateLimiterCanLimitByIPAndReportDenial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewBoundedRateLimiter(10)
	denied := 0
	router := gin.New()
	router.GET("/handoff/:id", limiter.Middleware("browser-handoff", 1, time.Minute, nil, func(*gin.Context) {
		denied++
	}), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for index, id := range []string{"first-ticket", "second-ticket"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/handoff/"+id, nil))
		want := http.StatusNoContent
		if index == 1 {
			want = http.StatusTooManyRequests
		}
		if recorder.Code != want {
			t.Fatalf("request %d status = %d, want %d", index, recorder.Code, want)
		}
	}
	if denied != 1 {
		t.Fatalf("denial callbacks = %d, want 1", denied)
	}
}
