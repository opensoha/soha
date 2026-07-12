package handlers

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func TestClearResponseWriteDeadlineKeepsRealStreamOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		if err := clearResponseWriteDeadline(c); err != nil {
			t.Errorf("clearResponseWriteDeadline() error = %v", err)
			return
		}
		_, _ = io.WriteString(c.Writer, "start\n")
		c.Writer.Flush()
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(c.Writer, "end\n")
		c.Writer.Flush()
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: time.Second,
		WriteTimeout:      50 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("shutdown stream server: %v", err)
		}
		if err := <-done; err != nil && err != http.ErrServerClosed {
			t.Errorf("serve stream server: %v", err)
		}
	})

	response, err := http.Get("http://" + listener.Addr().String() + "/stream")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if got := string(body); got != "start\nend\n" {
		t.Fatalf("stream body = %q, want start and end chunks", got)
	}
	if strings.TrimSpace(response.Header.Get("Connection")) == "close" {
		t.Fatal("stream was forced closed by the server write timeout")
	}
}

func (r *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadline = deadline
	return nil
}

func TestClearResponseWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &deadlineRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		deadline:         time.Now(),
	}
	ctx, _ := gin.CreateTestContext(recorder)

	if err := clearResponseWriteDeadline(ctx); err != nil {
		t.Fatalf("clearResponseWriteDeadline() error = %v", err)
	}
	if !recorder.deadline.IsZero() {
		t.Fatalf("deadline = %v, want zero", recorder.deadline)
	}
}
