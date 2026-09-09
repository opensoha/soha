package networkingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Service interface {
	Ingest(context.Context, networkidentity.Identity, []byte) (domainnetworkingest.Acknowledgement, error)
	IngestRADIUS(context.Context, networkidentity.Identity, []byte) (domainnetworkingest.Acknowledgement, error)
	Summary(context.Context, networkidentity.Identity, domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error)
}

type ReadyStore interface {
	Ping(context.Context) error
}

type Options struct {
	MaxBodyBytes      int64
	RequestsPerMinute int
}

type handler struct {
	service Service
	ready   ReadyStore
	options Options
	limiter *apiMiddleware.BoundedRateLimiter
}

func NewRouter(service Service, ready ReadyStore, options Options) (*gin.Engine, error) {
	if service == nil || ready == nil || options.MaxBodyBytes <= 0 || options.RequestsPerMinute <= 0 {
		return nil, fmt.Errorf("network ingest router dependencies and limits are required")
	}
	handler := &handler{service: service, ready: ready, options: options, limiter: apiMiddleware.NewBoundedRateLimiter(10_000)}
	router := gin.New()
	router.Use(gin.Recovery(), apiMiddleware.RequestID())
	router.GET("/healthz", handler.health)
	router.GET("/readyz", handler.readiness)
	router.POST("/api/ingest/v1/events:batch", handler.ingest)
	router.POST("/api/ingest/v1/radius/accounting", handler.radiusAccounting)
	router.GET("/api/ingest/v1/query/summary", handler.summary)
	return router, nil
}

func (h *handler) summary(c *gin.Context) {
	identity, err := authenticatedIdentity(c.Request)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "client_certificate_required", "a verified network ingest client certificate is required")
		return
	}
	if identity.Kind != "core" {
		apiresponse.Error(c, http.StatusForbidden, "ingest_query_identity_required", "a dedicated core ingest-query identity is required")
		return
	}
	allowed, retryAfter := h.limiter.Allow("query|"+identity.ID, h.options.RequestsPerMinute, time.Minute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		apiresponse.Error(c, http.StatusTooManyRequests, "rate_limited", "too many ingest queries")
		return
	}
	filter := domainnetworkingest.SummaryFilter{ProducerID: c.Query("producerId")}
	if raw := c.Query("from"); raw != "" {
		filter.From, err = time.Parse(time.RFC3339, raw)
	}
	if err == nil {
		if raw := c.Query("to"); raw != "" {
			filter.To, err = time.Parse(time.RFC3339, raw)
		}
	}
	if err == nil {
		if raw := c.Query("limit"); raw != "" {
			filter.Limit, err = strconv.Atoi(raw)
		}
	}
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_ingest_query", "the telemetry query is invalid")
		return
	}
	summary, err := h.service.Summary(c.Request.Context(), identity, filter)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.JSON(c, http.StatusOK, gin.H{"data": summary})
}

func (h *handler) health(c *gin.Context) {
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *handler) readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.ready.Ping(ctx); err != nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "postgres_unavailable", "network ingest storage is unavailable")
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ready"})
}

func (h *handler) ingest(c *gin.Context) {
	identity, raw, ok := h.request(c)
	if !ok {
		return
	}
	ack, err := h.service.Ingest(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, ack)
}

func (h *handler) radiusAccounting(c *gin.Context) {
	identity, raw, ok := h.request(c)
	if !ok {
		return
	}
	ack, err := h.service.IngestRADIUS(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, ack)
}

func (h *handler) request(c *gin.Context) (networkidentity.Identity, []byte, bool) {
	identity, err := authenticatedIdentity(c.Request)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "client_certificate_required", "a verified network ingest client certificate is required")
		return networkidentity.Identity{}, nil, false
	}
	allowed, retryAfter := h.limiter.Allow(identity.Kind+"|"+identity.ID, h.options.RequestsPerMinute, time.Minute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		apiresponse.Error(c, http.StatusTooManyRequests, "rate_limited", "too many ingest batches")
		return networkidentity.Identity{}, nil, false
	}
	if encoding := strings.TrimSpace(c.GetHeader("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		apiresponse.Error(c, http.StatusUnsupportedMediaType, "unsupported_content_encoding", "compressed ingest requests are not accepted")
		return networkidentity.Identity{}, nil, false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiresponse.Error(c, http.StatusUnsupportedMediaType, "json_content_type_required", "Content-Type must be application/json")
		return networkidentity.Identity{}, nil, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.options.MaxBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			apiresponse.Error(c, http.StatusRequestEntityTooLarge, "ingest_body_too_large", "ingest request body exceeds the configured limit")
			return networkidentity.Identity{}, nil, false
		}
		apiresponse.Error(c, http.StatusBadRequest, "invalid_request_body", "ingest request body could not be read")
		return networkidentity.Identity{}, nil, false
	}
	return identity, raw, true
}

func authenticatedIdentity(request *http.Request) (networkidentity.Identity, error) {
	if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.VerifiedChains[0]) == 0 {
		return networkidentity.Identity{}, fmt.Errorf("verified client certificate is required")
	}
	return networkidentity.ParseCertificate(request.TLS.VerifiedChains[0][0], networkidentity.ScopeIngest)
}

func respondError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, apperrors.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, apperrors.ErrInvalidArgument):
		status = http.StatusBadRequest
	case errors.Is(err, apperrors.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, apperrors.ErrServiceUnavailable):
		status = http.StatusServiceUnavailable
	}
	code, message := "internal_error", "network ingest request failed"
	var business *apperrors.BusinessError
	if errors.As(err, &business) {
		code = business.Code()
		message = business.Message(c.GetHeader("Accept-Language"))
	}
	apiresponse.Error(c, status, code, message)
}
