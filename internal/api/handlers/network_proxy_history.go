package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
	"strconv"
)

type networkProxyHistory interface {
	ConfigurationHistory(context.Context, domainidentity.Principal, string) ([]domain.ConfigurationRevision, error)
	RestoreConfiguration(context.Context, domainidentity.Principal, string, int64, int64) (domain.Instance, error)
}

func (h *NetworkProxyHandler) ConfigurationHistory(c *gin.Context) {
	service, ok := h.service.(networkProxyHistory)
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := service.ConfigurationHistory(c.Request.Context(), principal(c), c.Param("instanceID"))
	c.Header("Cache-Control", "no-store")
	respondItems(c, items, err)
}
func (h *NetworkProxyHandler) RestoreConfiguration(c *gin.Context) {
	service, ok := h.service.(networkProxyHistory)
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	revision, err := strconv.ParseInt(c.Param("revision"), 10, 64)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	var input sohaapi.NetworkProxyRestoreInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := service.RestoreConfiguration(c.Request.Context(), principal(c), c.Param("instanceID"), revision, int64(input.ExpectedRevision))
	respondItem(c, http.StatusOK, item, err)
}
