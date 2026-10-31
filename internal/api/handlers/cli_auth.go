package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type IdentityCLIDeviceService interface {
	CLIAuthOptions(context.Context) domainidentity.CLIAuthOptions
	CreateCLIDeviceAuthorization(context.Context, domainidentity.CLIDeviceCreate, string) (domainidentity.CLIDeviceAuthorization, error)
	InspectCLIDeviceAuthorization(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, domainidentity.CLIDeviceInspect) (domainidentity.CLIDeviceConfirmation, error)
	DecideCLIDeviceAuthorization(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, domainidentity.CLIDeviceDecision) (domainidentity.CLIDeviceStatus, error)
	ExchangeCLIDeviceToken(context.Context, domainidentity.CLIDeviceTokenRequest) (domainidentity.AuthResult, error)
	CancelCLIDeviceAuthorization(context.Context, domainidentity.CLIDeviceCancelRequest) (domainidentity.CLIDeviceStatus, error)
}

type IdentityCLISessionService interface {
	LoginCLIWithPassword(context.Context, string, string, string) (domainidentity.AuthResult, error)
	RefreshCLISession(context.Context, domainidentity.CLIRefreshRequest) (domainidentity.AuthResult, error)
	LogoutCLISession(context.Context, string) error
	GetAuthContext(context.Context, domainidentity.Principal, domainidentity.AccessContext) (domainidentity.CLIAuthContext, error)
	ListCLISessions(context.Context, domainidentity.Principal, domainidentity.AccessContext, int, string) (domainidentity.CLISessionPage, error)
	RevokeOwnCLISession(context.Context, domainidentity.Principal, domainidentity.AccessContext, string) error
}

type CLIAuthHandler struct {
	devices  IdentityCLIDeviceService
	sessions IdentityCLISessionService
}

func NewCLIAuthHandler(devices IdentityCLIDeviceService, sessions IdentityCLISessionService) (*CLIAuthHandler, error) {
	for _, service := range []any{devices, sessions} {
		if service == nil {
			return nil, fmt.Errorf("CLI authentication handler requires device and session services")
		}
		value := reflect.ValueOf(service)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return nil, fmt.Errorf("CLI authentication handler requires non-nil services")
			}
		}
	}
	return &CLIAuthHandler{devices: devices, sessions: sessions}, nil
}

const maxCLIAuthRequestBytes = 256 << 10

func cliAuthHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
}
func bindCLIAuthJSON(c *gin.Context, destination any) bool {
	cliAuthHeaders(c)
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "CLI authentication requires a JSON request")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCLIAuthRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(destination)
	if err == nil {
		err = decoder.Decode(&struct{}{})
		if errors.Is(err, io.EOF) {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		apiresponse.Error(c, http.StatusRequestEntityTooLarge, "payload_too_large", "CLI authentication request exceeds the size limit")
		return false
	}
	apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Invalid CLI authentication request")
	return false
}
func cliBrowserOrigin(c *gin.Context) string {
	values := c.Request.Header.Values("Origin")
	if len(values) != 1 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
func writeCLIAuthError(c *gin.Context, err error) {
	var poll interface {
		error
		RetryAfterSeconds() int
		PublicCode() string
		AuthorizationPending() bool
	}
	if errors.As(err, &poll) {
		c.Header("Retry-After", strconv.Itoa(max(1, poll.RetryAfterSeconds())))
		status := http.StatusTooManyRequests
		if poll.AuthorizationPending() {
			status = http.StatusConflict
		}
		apiresponse.Error(c, status, poll.PublicCode(), poll.PublicCode())
		return
	}
	writeError(c, err)
}
func writeCLIAuthResult(c *gin.Context, value any, err error) {
	cliAuthHeaders(c)
	if err != nil {
		writeCLIAuthError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, value)
}
func (h *CLIAuthHandler) Options(c *gin.Context) {
	writeCLIAuthResult(c, h.devices.CLIAuthOptions(c.Request.Context()), nil)
}
func (h *CLIAuthHandler) Create(c *gin.Context) {
	var input domainidentity.CLIDeviceCreate
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.devices.CreateCLIDeviceAuthorization(c.Request.Context(), input, c.ClientIP())
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Inspect(c *gin.Context) {
	var input domainidentity.CLIDeviceInspect
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.devices.InspectCLIDeviceAuthorization(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), cliBrowserOrigin(c), c.ClientIP(), input)
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Decide(c *gin.Context) {
	var input domainidentity.CLIDeviceDecision
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.devices.DecideCLIDeviceAuthorization(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), cliBrowserOrigin(c), c.ClientIP(), input)
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Exchange(c *gin.Context) {
	var input domainidentity.CLIDeviceTokenRequest
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.devices.ExchangeCLIDeviceToken(c.Request.Context(), input)
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Cancel(c *gin.Context) {
	var input domainidentity.CLIDeviceCancelRequest
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.devices.CancelCLIDeviceAuthorization(c.Request.Context(), input)
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Password(c *gin.Context) {
	var input dto.PasswordLoginRequest
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.sessions.LoginCLIWithPassword(c.Request.Context(), input.Login, input.Password, c.ClientIP())
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Refresh(c *gin.Context) {
	var input domainidentity.CLIRefreshRequest
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	result, err := h.sessions.RefreshCLISession(c.Request.Context(), input)
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) Logout(c *gin.Context) {
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !bindCLIAuthJSON(c, &input) {
		return
	}
	if err := h.sessions.LogoutCLISession(c.Request.Context(), input.RefreshToken); err != nil {
		writeCLIAuthError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
func (h *CLIAuthHandler) Context(c *gin.Context) {
	result, err := h.sessions.GetAuthContext(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c))
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) ListSessions(c *gin.Context) {
	cliAuthHeaders(c)
	limit := 20
	if raw, ok := c.GetQuery("limit"); ok {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Invalid session page size")
			return
		}
		limit = parsed
	}
	result, err := h.sessions.ListCLISessions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), limit, c.Query("cursor"))
	writeCLIAuthResult(c, result, err)
}
func (h *CLIAuthHandler) RevokeSession(c *gin.Context) {
	cliAuthHeaders(c)
	if err := h.sessions.RevokeOwnCLISession(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("sessionID")); err != nil {
		writeCLIAuthError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
