package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func clearResponseWriteDeadline(c *gin.Context) error {
	err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
