package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (r *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadline = deadline
	return nil
}

func TestClearResponseWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), deadline: time.Now()}
	ctx, _ := gin.CreateTestContext(recorder)

	if err := clearResponseWriteDeadline(ctx); err != nil {
		t.Fatalf("clearResponseWriteDeadline() error = %v", err)
	}
	if !recorder.deadline.IsZero() {
		t.Fatalf("deadline = %v, want zero", recorder.deadline)
	}
}

func TestClearResponseWriteDeadlineOverridesServerTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		if err := clearResponseWriteDeadline(c); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		time.Sleep(60 * time.Millisecond)
		_, _ = c.Writer.WriteString("ok")
	})
	server := httptest.NewUnstartedServer(router)
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/stream")
	if err != nil {
		t.Fatalf("GET /stream: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response = %d %q, want 200 %q", response.StatusCode, body, "ok")
	}
}
