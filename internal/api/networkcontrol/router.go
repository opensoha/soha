package networkcontrol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Service interface {
	Ready() bool
	Enroll(context.Context, networkidentity.Identity, string, []byte) (networkprotocol.RuntimeMessage, error)
	Configuration(context.Context, networkidentity.Identity) (networkprotocol.RuntimeMessage, error)
	MihomoSource(context.Context, networkidentity.Identity, string) (networkprotocol.MihomoSource, error)
	MihomoSubscription(context.Context, networkidentity.Identity, string) (networkprotocol.MihomoSubscription, error)
	Apply(context.Context, networkidentity.Identity, []byte) error
	Renew(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error)
	Revoke(context.Context, networkidentity.Identity, []byte) (int64, error)
	ConnectVPN(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error)
	AuthorizeNAS(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error)
	AuthorizeRADIUS(context.Context, networkidentity.Identity, []byte) (networkprotocol.NASAuthorizationResult, error)
	NextNASSessionCommand(context.Context, networkidentity.Identity) (networkprotocol.RuntimeMessage, error)
	CompleteNASSessionCommand(context.Context, networkidentity.Identity, []byte) error
	Snapshot(context.Context, networkidentity.Identity) (domainnetworkruntime.PolicySnapshot, error)
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
		return nil, fmt.Errorf("network control router dependencies and limits are required")
	}
	handler := &handler{service: service, ready: ready, options: options, limiter: apiMiddleware.NewBoundedRateLimiter(10_000)}
	router := gin.New()
	router.Use(gin.Recovery(), apiMiddleware.RequestID())
	router.GET("/healthz", handler.health)
	router.GET("/readyz", handler.readiness)
	router.GET("/api/network-control/v1/runtimes/:runtimeID/configuration", handler.configuration)
	router.GET("/api/network-control/v1/runtimes/:runtimeID/mihomo-profiles/:profileID/source", handler.mihomoSource)
	router.GET("/api/network-control/v1/runtimes/:runtimeID/mihomo-profiles/:profileID/subscription", handler.mihomoSubscription)
	router.GET("/api/network-control/v1/runtimes/:runtimeID/nas-session-commands/next", handler.nextNASSessionCommand)
	router.POST("/api/network-control/v1/runtimes/:runtimeID/*action", handler.action)
	router.GET("/api/network-control/v1/snapshots/current", handler.snapshot)
	return router, nil
}

func (h *handler) action(c *gin.Context) {
	switch c.Param("action") {
	case "/enroll":
		h.enroll(c)
	case "/configuration:applied":
		h.apply(c)
	case "/leases:renew":
		h.renew(c)
	case "/leases:revoke":
		h.revoke(c)
	case "/vpn:connect":
		h.connectVPN(c)
	case "/nas:authorize":
		h.authorizeNAS(c)
	case "/radius:post-auth":
		h.radiusPostAuth(c)
	case "/nas-session-commands/result":
		h.completeNASSessionCommand(c)
	default:
		apiresponse.Error(c, http.StatusNotFound, "not_found", "network control route not found")
	}
}

func (h *handler) health(c *gin.Context) {
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *handler) readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.ready.Ping(ctx); err != nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "postgres_unavailable", "network control storage is unavailable")
		return
	}
	if !h.service.Ready() {
		apiresponse.Error(c, http.StatusServiceUnavailable, "policy_snapshot_unavailable", "network policy snapshot is not loaded")
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ready"})
}

func (h *handler) enroll(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	token, ok := bearerToken(c.Request)
	if !ok {
		apiresponse.Error(c, http.StatusUnauthorized, "enrollment_token_required", "exactly one bearer enrollment token is required")
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	result, err := h.service.Enroll(c.Request.Context(), identity, token, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusCreated, result)
}

func (h *handler) configuration(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	result, err := h.service.Configuration(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) mihomoSubscription(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	result, err := h.service.MihomoSubscription(c.Request.Context(), identity, c.Param("profileID"))
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) mihomoSource(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	result, err := h.service.MihomoSource(c.Request.Context(), identity, c.Param("profileID"))
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) apply(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	if err := h.service.Apply(c.Request.Context(), identity, raw); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handler) renew(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	result, err := h.service.Renew(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) revoke(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	revoked, err := h.service.Revoke(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"revokedLeaseCount": revoked})
}

func (h *handler) connectVPN(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	result, err := h.service.ConnectVPN(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) authorizeNAS(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	result, err := h.service.AuthorizeNAS(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

type radiusReplyAttribute struct {
	DoXLAT bool     `json:"do_xlat"`
	Op     string   `json:"op"`
	Value  []string `json:"value"`
}

func (h *handler) radiusPostAuth(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	result, err := h.service.AuthorizeRADIUS(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	if result.Decision != "allow" {
		apiresponse.Error(c, http.StatusForbidden, "network_access_denied", "network access denied")
		return
	}
	if result.RadiusAttributes == nil || result.RadiusAttributes.SessionTimeoutSeconds <= 0 {
		apiresponse.Error(c, http.StatusServiceUnavailable, "radius_attributes_unavailable", "network access attributes are unavailable")
		return
	}
	attribute := func(value string) radiusReplyAttribute {
		return radiusReplyAttribute{DoXLAT: false, Op: ":=", Value: []string{value}}
	}
	attributes := map[string]radiusReplyAttribute{
		"reply:Session-Timeout": attribute(fmt.Sprint(result.RadiusAttributes.SessionTimeoutSeconds)),
		"reply:Class":           attribute(result.SessionID),
	}
	if result.RadiusAttributes.VLANID != 0 {
		attributes["reply:Tunnel-Type"] = attribute("VLAN")
		attributes["reply:Tunnel-Medium-Type"] = attribute("IEEE-802")
		attributes["reply:Tunnel-Private-Group-Id"] = attribute(fmt.Sprint(result.RadiusAttributes.VLANID))
	}
	if result.RadiusAttributes.FilterID != "" {
		attributes["reply:Filter-Id"] = attribute(result.RadiusAttributes.FilterID)
	}
	apiresponse.JSON(c, http.StatusOK, attributes)
}

func (h *handler) nextNASSessionCommand(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	result, err := h.service.NextNASSessionCommand(c.Request.Context(), identity)
	if errors.Is(err, apperrors.ErrNotFound) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) completeNASSessionCommand(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	if err := h.service.CompleteNASSessionCommand(c.Request.Context(), identity, raw); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handler) snapshot(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	snapshot, err := h.service.Snapshot(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, snapshot)
}

func (h *handler) identity(c *gin.Context) (networkidentity.Identity, bool) {
	identity, err := authenticatedIdentity(c.Request)
	if err != nil {
		apiresponse.Error(c, http.StatusUnauthorized, "client_certificate_required", "a verified network control client certificate is required")
		return networkidentity.Identity{}, false
	}
	if runtimeID := c.Param("runtimeID"); runtimeID != "" && runtimeID != identity.ID {
		apiresponse.Error(c, http.StatusUnauthorized, "runtime_identity_mismatch", "the authenticated runtime does not match the request path")
		return networkidentity.Identity{}, false
	}
	allowed, retryAfter := h.limiter.Allow(identity.Kind+"|"+identity.ID, h.options.RequestsPerMinute, time.Minute)
	if !allowed {
		c.Header("Retry-After", fmt.Sprint(retryAfter))
		apiresponse.Error(c, http.StatusTooManyRequests, "rate_limited", "too many network control requests")
		return networkidentity.Identity{}, false
	}
	return identity, true
}

func (h *handler) body(c *gin.Context) ([]byte, bool) {
	if encoding := strings.TrimSpace(c.GetHeader("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		apiresponse.Error(c, http.StatusUnsupportedMediaType, "unsupported_content_encoding", "compressed network control requests are not accepted")
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiresponse.Error(c, http.StatusUnsupportedMediaType, "json_content_type_required", "Content-Type must be application/json")
		return nil, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.options.MaxBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			apiresponse.Error(c, http.StatusRequestEntityTooLarge, "control_body_too_large", "network control request body exceeds the configured limit")
			return nil, false
		}
		apiresponse.Error(c, http.StatusBadRequest, "invalid_request_body", "network control request body could not be read")
		return nil, false
	}
	return raw, true
}

func authenticatedIdentity(request *http.Request) (networkidentity.Identity, error) {
	if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.VerifiedChains[0]) == 0 {
		return networkidentity.Identity{}, fmt.Errorf("verified client certificate is required")
	}
	return networkidentity.ParseCertificate(request.TLS.VerifiedChains[0][0], networkidentity.ScopeNetworkControl)
}

func bearerToken(request *http.Request) (string, bool) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], parts[1] != ""
}

func respondError(c *gin.Context, err error) {
	apiresponse.Error(c, apierrors.StatusCode(err), apierrors.Code(err), apierrors.Message(err, c.GetHeader("Accept-Language")))
}
