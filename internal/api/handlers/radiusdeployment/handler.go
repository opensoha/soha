package radiusdeployment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	"github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
	"strings"
)

type Management interface {
	List(context.Context, identity.Principal) (api.NetworkRADIUSPoolList, error)
	Options(context.Context, identity.Principal) (api.NetworkRADIUSOptions, error)
	Get(context.Context, identity.Principal, string) (api.NetworkRADIUSPool, error)
	Create(context.Context, identity.Principal, api.NetworkRADIUSPoolInput) (api.NetworkRADIUSPool, error)
	Configure(context.Context, identity.Principal, string, api.NetworkRADIUSConfigurationInput) (api.NetworkRADIUSPool, error)
	Control(context.Context, identity.Principal, string, api.NetworkRADIUSControlInput) (api.NetworkRADIUSPool, error)
}
type Handler struct {
	management Management
	schemas    *networkprotocol.Schemas
}

func New(m Management, s *networkprotocol.Schemas) *Handler {
	if s == nil {
		s, _ = networkprotocol.CompileSchemas()
	}
	return &Handler{m, s}
}
func (h *Handler) List(c *gin.Context) {
	v, e := h.management.List(c.Request.Context(), middleware.PrincipalFromContext(c))
	respond(c, 200, v, e)
}
func (h *Handler) Options(c *gin.Context) {
	v, e := h.management.Options(c.Request.Context(), middleware.PrincipalFromContext(c))
	respond(c, 200, v, e)
}
func (h *Handler) Get(c *gin.Context) {
	v, e := h.management.Get(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("poolID"))
	respond(c, 200, v, e)
}
func (h *Handler) Create(c *gin.Context) {
	var i api.NetworkRADIUSPoolInput
	if h.schemas == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	if !decode(c, &i, h.schemas) {
		return
	}
	v, e := h.management.Create(c.Request.Context(), middleware.PrincipalFromContext(c), i)
	respond(c, 202, v, e)
}
func (h *Handler) Configure(c *gin.Context) {
	var i api.NetworkRADIUSConfigurationInput
	if h.schemas == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	if !decode(c, &i, h.schemas) {
		return
	}
	v, e := h.management.Configure(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("poolID"), i)
	respond(c, 202, v, e)
}
func (h *Handler) Control(c *gin.Context) {
	var i api.NetworkRADIUSControlInput
	if h.schemas == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	if !decode(c, &i, h.schemas) {
		return
	}
	v, e := h.management.Control(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("poolID"), i)
	respond(c, 202, v, e)
}
func fail(c *gin.Context, e error) {
	response.Error(c, apierrors.StatusCode(e), apierrors.Code(e), apierrors.Message(e, apierrors.RequestLanguage(c.Request)))
}
func respond(c *gin.Context, status int, v any, e error) {
	c.Header("Cache-Control", "no-store")
	if e != nil {
		fail(c, e)
		return
	}
	response.Item(c, status, v)
}
func decode(c *gin.Context, dst any, s *networkprotocol.Schemas) bool {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	if e != nil || (s != nil && s.ValidateRadiusRuntime(raw) != nil) {
		fail(c, apperrors.ErrInvalidArgument)
		return false
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(dst) == nil {
		var extra any
		if errors.Is(d.Decode(&extra), io.EOF) {
			return true
		}
	}
	fail(c, apperrors.ErrInvalidArgument)
	return false
}

type Executor interface {
	Claim(context.Context, string, string) (delivery.ExecutionTask, error)
	Status(context.Context, string, string) (delivery.ExecutionTask, error)
	Callback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)
}
type ExecutorHandler struct{ executor Executor }

func NewExecutor(e Executor) *ExecutorHandler { return &ExecutorHandler{e} }
func token(c *gin.Context) (string, bool) {
	a := c.Request.Header.Values("Authorization")
	if len(a) == 1 {
		p := strings.Fields(a[0])
		if len(p) == 2 && strings.EqualFold(p[0], "Bearer") {
			return p[1], true
		}
	}
	fail(c, apperrors.ErrUnauthorized)
	return "", false
}
func (h *ExecutorHandler) Claim(c *gin.Context) {
	t, ok := token(c)
	if !ok {
		return
	}
	var i struct {
		ExecutorID string `json:"executorId"`
	}
	if !decode(c, &i, nil) {
		return
	}
	v, e := h.executor.Claim(c.Request.Context(), t, i.ExecutorID)
	if errors.Is(e, apperrors.ErrNotFound) {
		c.Status(204)
		return
	}
	respond(c, 202, v, e)
}
func (h *ExecutorHandler) Status(c *gin.Context) {
	t, ok := token(c)
	if !ok {
		return
	}
	q := c.Request.URL.Query()["taskId"]
	if len(q) != 1 || q[0] == "" {
		fail(c, apperrors.ErrInvalidArgument)
		return
	}
	v, e := h.executor.Status(c.Request.Context(), t, q[0])
	respond(c, 200, v, e)
}
func (h *ExecutorHandler) Callback(c *gin.Context) {
	t, ok := token(c)
	if !ok {
		return
	}
	var i api.ExecutionCallbackRequest
	if !decode(c, &i, nil) {
		return
	}
	v, e := h.executor.Callback(c.Request.Context(), t, delivery.ExecutionCallbackInput{CallbackToken: i.CallbackToken, Status: i.Status, Payload: i.Payload})
	respond(c, 200, v, e)
}
