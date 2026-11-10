package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

// These Delivery contracts require data; existing raw list consumers use items.
func writeDeliveryList[T any](c *gin.Context, items []T) {
	if items == nil {
		items = []T{}
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"data": items, "items": items})
}
