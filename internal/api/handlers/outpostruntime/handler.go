package outpostruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	"github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	app "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Management interface {
	View(context.Context, identity.Principal, string) (app.View, error)
	DescribeState(context.Context, domain.State) (app.View, error)
	Bind(context.Context, identity.Principal, string, domain.BindingInput) (domain.State, error)
	Control(context.Context, identity.Principal, string, domain.ControlInput) (domain.State, error)
}

type Executor interface {
	Claim(context.Context, string, string) (delivery.ExecutionTask, error)
	Status(context.Context, string, string) (delivery.ExecutionTask, error)
	Callback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)
}

type Handler struct {
	management Management
	executor   Executor
}

func New(management Management, executor Executor) *Handler {
	return &Handler{management: management, executor: executor}
}

func (h *Handler) Get(c *gin.Context) {
	view, err := h.management.View(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("outpostID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, runtimeDTO(view))
}

func (h *Handler) Bind(c *gin.Context) {
	var input sohaapi.IdentityOutpostRuntimeBindingInput
	if !decode(c, &input) {
		return
	}
	state, err := h.management.Bind(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("outpostID"), domain.BindingInput{
		SlotID: input.SlotID, ExpectedGeneration: input.ExpectedGeneration, RequestID: input.RequestID, ExplicitAdoption: input.ExplicitAdoption,
	})
	h.mutation(c, state, err)
}

func (h *Handler) Control(c *gin.Context) {
	var input sohaapi.IdentityOutpostRuntimeControlInput
	if !decode(c, &input) {
		return
	}
	target := domain.ControlInput{Action: string(input.Action), ExpectedGeneration: input.ExpectedGeneration, RequestID: input.RequestID}
	if input.Replicas != nil {
		if *input.Replicas < 0 || *input.Replicas > math.MaxInt32 {
			writeError(c, apperrors.ErrInvalidArgument)
			return
		}
		replicas := int32(*input.Replicas)
		target.Replicas = &replicas
	}
	if input.DrainTimeoutSeconds != nil {
		target.DrainTimeoutSeconds = *input.DrainTimeoutSeconds
	}
	state, err := h.management.Control(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("outpostID"), target)
	h.mutation(c, state, err)
}

func (h *Handler) mutation(c *gin.Context, state domain.State, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	view, err := h.management.DescribeState(c.Request.Context(), state)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusAccepted, sohaapi.IdentityOutpostRuntimeMutationResult{Runtime: runtimeDTO(view)})
}

func (h *Handler) Claim(c *gin.Context) {
	token, ok := executorToken(c)
	if !ok {
		return
	}
	var input sohaapi.IdentityOutpostRuntimeExecutorClaimRequest
	if !decode(c, &input) {
		return
	}
	task, err := h.executor.Claim(c.Request.Context(), token, input.ExecutorID)
	if errors.Is(err, apperrors.ErrNotFound) {
		c.Status(http.StatusNoContent)
		return
	}
	writeTask(c, http.StatusAccepted, task, err)
}

func (h *Handler) Status(c *gin.Context) {
	token, ok := executorToken(c)
	if !ok {
		return
	}
	ids := c.Request.URL.Query()["taskId"]
	if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	task, err := h.executor.Status(c.Request.Context(), token, ids[0])
	writeTask(c, http.StatusOK, task, err)
}

func (h *Handler) Callback(c *gin.Context) {
	token, ok := executorToken(c)
	if !ok {
		return
	}
	var input sohaapi.ExecutionCallbackRequest
	if !decode(c, &input) {
		return
	}
	task, err := h.executor.Callback(c.Request.Context(), token, delivery.ExecutionCallbackInput{CallbackToken: input.CallbackToken, Status: input.Status, Payload: input.Payload})
	writeTask(c, http.StatusOK, task, err)
}

func runtimeDTO(view app.View) sohaapi.IdentityOutpostRuntime {
	item := sohaapi.IdentityOutpostRuntime{
		OutpostID: view.OutpostID, Owner: sohaapi.IdentityOutpostRuntimeOwner(view.Owner), SlotID: view.SlotID,
		Namespace: view.Namespace, WorkloadName: view.WorkloadName, WorkloadUID: view.WorkloadUID,
		Generation: view.Generation, DesiredReplicas: int(view.DesiredReplicas), Phase: sohaapi.IdentityOutpostRuntimePhase(view.Phase),
		Reason: view.Reason, LatestTaskID: view.LatestTaskID, ObservedAt: view.ObservedAt,
		Capabilities: sohaapi.IdentityOutpostRuntimeCapabilities{SupportsScale: view.SupportsScale, SupportsDrain: view.SupportsDrain},
	}
	if view.ObservedAt != nil {
		item.ObservedGeneration = &view.ObservedGeneration
	}
	if view.ActualReplicas != nil {
		actual := int(*view.ActualReplicas)
		item.ActualReplicas = &actual
	}
	if view.ReadyReplicas != nil {
		ready := int(*view.ReadyReplicas)
		item.ReadyReplicas = &ready
	}
	return item
}

func executorToken(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Authorization")
	if len(values) == 1 {
		parts := strings.Fields(values[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1], true
		}
	}
	writeError(c, apperrors.ErrUnauthorized)
	return "", false
}

func decode(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) == nil {
		var extra any
		if errors.Is(decoder.Decode(&extra), io.EOF) {
			return true
		}
	}
	writeError(c, apperrors.ErrInvalidArgument)
	return false
}

func writeTask(c *gin.Context, status int, task delivery.ExecutionTask, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, status, task)
}

func writeError(c *gin.Context, err error) {
	apiresponse.Error(c, apierrors.StatusCode(err), apierrors.Code(err), apierrors.Message(err, apierrors.RequestLanguage(c.Request)))
}
