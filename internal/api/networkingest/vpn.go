package networkingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	response "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type VPNQueryService interface {
	VPNProbe(context.Context, networkidentity.Identity, string, string, string) (*networkprotocol.VPNProbeBatch, error)
	VPNMetrics(context.Context, networkidentity.Identity, domain.VPNMetricsQuery) (domain.VPNMetrics, error)
}

func (h *handler) vpnProbe(c *gin.Context) {
	identity, err := authenticatedIdentity(c.Request)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, "client_certificate_required", "verified ingest-query certificate required")
		return
	}
	if identity.Kind != "core" && identity.Kind != "network-control" {
		response.Error(c, http.StatusForbidden, "ingest_query_identity_required", "ingest-query identity required")
		return
	}
	allowed, retryAfter := h.limiter.Allow("query|"+identity.Kind+"|"+identity.ID, h.options.RequestsPerMinute, time.Minute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		response.Error(c, http.StatusTooManyRequests, "rate_limited", "too many ingest queries")
		return
	}
	service, ok := h.service.(VPNQueryService)
	if !ok {
		response.Error(c, http.StatusServiceUnavailable, "vpn_query_unavailable", "VPN telemetry query unavailable")
		return
	}
	result, err := service.VPNProbe(c.Request.Context(), identity, c.Query("producerId"), c.Query("intentId"), c.Query("batchId"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.JSON(c, http.StatusOK, gin.H{"data": result})
}

func (h *handler) vpnMetrics(c *gin.Context) {
	identity, raw, ok := h.request(c)
	if !ok {
		return
	}
	if identity.Kind != "core" {
		response.Error(c, http.StatusForbidden, "ingest_query_identity_required", "dedicated core ingest-query identity required")
		return
	}
	var query domain.VPNMetricsQuery
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&query) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		response.Error(c, http.StatusBadRequest, "invalid_ingest_query", "invalid VPN metric query")
		return
	}
	service, ok := h.service.(VPNQueryService)
	if !ok {
		response.Error(c, http.StatusServiceUnavailable, "vpn_query_unavailable", "VPN telemetry query unavailable")
		return
	}
	result, err := service.VPNMetrics(c.Request.Context(), identity, query)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.JSON(c, http.StatusOK, gin.H{"data": result})
}
