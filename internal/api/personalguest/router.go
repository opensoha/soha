package personalguest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Service interface {
	Enroll(context.Context, networkidentity.Identity, string, guest.Enrollment) (guest.Binding, error)
	Binding(context.Context, networkidentity.Identity) (guest.Binding, error)
	Heartbeat(context.Context, networkidentity.Identity, guest.Heartbeat) error
}

type handler struct {
	service Service
	limiter *apiMiddleware.BoundedRateLimiter
}

// This router must be served behind verified client TLS, never trusted headers.
func NewRouter(service Service) (*gin.Engine, error) {
	if nilService(service) {
		return nil, fmt.Errorf("personal guest service is required")
	}
	h := &handler{service: service, limiter: apiMiddleware.NewBoundedRateLimiter(10_000)}
	router := gin.New()
	router.Use(gin.Recovery(), apiMiddleware.RequestID())
	router.GET(guest.BindingsPath+"/:bindingID", h.binding)
	router.POST(guest.BindingsPath+"/:bindingID/enroll", h.enroll)
	router.POST(guest.BindingsPath+"/:bindingID/heartbeats", h.heartbeat)
	router.POST(guest.BindingsPath+"/:bindingID/renewals", h.renew)
	router.POST(guest.BindingsPath+"/:bindingID/renewals/:requestID/commit", h.commitRenewal)
	router.GET(guest.BindingsPath+"/:bindingID/artifact", h.artifact)
	router.GET(guest.BindingsPath+"/:bindingID/data-operations/:operationID", h.dataOperation)
	router.GET(guest.BindingsPath+"/:bindingID/data-lease", h.dataLease)
	router.GET(guest.BindingsPath+"/:bindingID/tools-operations/:operationID", h.toolsOperation)
	router.GET(guest.BindingsPath+"/:bindingID/runtime-grants/:operationID", h.runtimeOperation)
	router.POST(guest.BindingsPath+"/:bindingID/browser-network/:operationID", h.browserNetwork)
	return router, nil
}

func nilService(service Service) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (h *handler) identity(c *gin.Context) (networkidentity.Identity, bool) {
	c.Header("Cache-Control", "no-store")
	tls := c.Request.TLS
	if tls == nil || len(tls.VerifiedChains) == 0 || len(tls.VerifiedChains[0]) == 0 {
		respondError(c, apperrors.ErrUnauthorized)
		return networkidentity.Identity{}, false
	}
	leaf := tls.VerifiedChains[0][0]
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		respondError(c, apperrors.ErrUnauthorized)
		return networkidentity.Identity{}, false
	}
	identity, err := networkidentity.ParseCertificate(leaf, networkidentity.ScopePersonalGuest)
	if err != nil || identity.ID != c.Param("bindingID") {
		respondError(c, apperrors.ErrUnauthorized)
		return networkidentity.Identity{}, false
	}
	allowed, retryAfter := h.limiter.Allow(identity.ID, 60, time.Minute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		apiresponse.Error(c, http.StatusTooManyRequests, "rate_limited", "too many personal guest requests")
		return networkidentity.Identity{}, false
	}
	return identity, true
}

func (h *handler) enroll(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	headers := c.Request.Header.Values("Authorization")
	if len(headers) != 1 {
		respondError(c, apperrors.ErrUnauthorized)
		return
	}
	var request guest.Enrollment
	if !readBody(c, &request) {
		return
	}
	result, err := h.service.Enroll(c.Request.Context(), identity, headers[0], request)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusCreated, result)
}

func (h *handler) binding(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	result, err := h.service.Binding(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) heartbeat(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	var request guest.Heartbeat
	if !readBody(c, &request) {
		return
	}
	if err := h.service.Heartbeat(c.Request.Context(), identity, request); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func readBody(c *gin.Context, value any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" || (c.GetHeader("Content-Encoding") != "" && c.GetHeader("Content-Encoding") != "identity") {
		apiresponse.Error(c, http.StatusUnsupportedMediaType, "json_content_type_required", "uncompressed application/json is required")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if len(raw) > 4096 {
		apiresponse.Error(c, http.StatusRequestEntityTooLarge, "body_too_large", "personal guest request exceeds the limit")
		return false
	}
	if err != nil || json.Unmarshal(raw, value) != nil {
		respondError(c, apperrors.ErrInvalidArgument)
		return false
	}
	return true
}

func respondError(c *gin.Context, err error) {
	apiresponse.Error(c, apierrors.StatusCode(err), apierrors.Code(err), apierrors.Message(err, c.GetHeader("Accept-Language")))
}
