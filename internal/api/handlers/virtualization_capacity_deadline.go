package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func capacityReadContext(c *gin.Context) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	deadline, _ := ctx.Deadline()
	// Capacity traverses provider inventory; leave time to write a bounded error or result.
	err := http.NewResponseController(c.Writer).SetWriteDeadline(deadline.Add(5 * time.Second))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		cancel()
		return nil, nil, err
	}
	return ctx, cancel, nil
}
