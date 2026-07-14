package handlers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaieval "github.com/opensoha/soha/internal/application/aieval"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	evaluationDatasetSchema = "opensoha.dev/evaluation-dataset/v1"
	evaluationRunSchema     = "opensoha.dev/evaluation-run/v1"
)

var evaluationDatasetIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,127}$`)

type EvaluationService interface {
	ListDatasets(context.Context) ([]appaieval.Dataset, error)
	PutDataset(context.Context, appaieval.Dataset) error
	ListRuns(context.Context) ([]appaieval.Run, error)
	GetRun(context.Context, string) (appaieval.Run, error)
	StartRun(context.Context, appaieval.Run, time.Time) (appaieval.Run, error)
	CompleteRun(context.Context, string, []appaieval.SampleOutput, time.Time) (appaieval.Run, error)
}

type EvaluationAuthorizer interface {
	Authorize(context.Context, domainidentity.Principal, string) error
	AuthorizeAny(context.Context, domainidentity.Principal, ...string) error
}

type EvaluationHandler struct {
	service EvaluationService
	auth    EvaluationAuthorizer
	now     func() time.Time
}

func NewEvaluationHandler(service EvaluationService, authorizer EvaluationAuthorizer) *EvaluationHandler {
	return &EvaluationHandler{service: service, auth: authorizer, now: time.Now}
}

func RegisterEvaluationRoutes(group gin.IRoutes, handler *EvaluationHandler) {
	group.GET("/ai/evaluations/datasets", handler.listDatasets)
	group.POST("/ai/evaluations/datasets", handler.createDataset)
	group.GET("/ai/evaluations/runs", handler.listRuns)
	group.POST("/ai/evaluations/runs", handler.startRun)
	group.GET("/ai/evaluations/runs/:runID", handler.getRun)
	group.GET("/ai/evaluations/runs/:runID/results", handler.listRunResults)
	group.POST("/ai/evaluations/runs/:runID/complete", handler.completeRun)
}

func (h *EvaluationHandler) listDatasets(c *gin.Context) {
	if !h.requireRead(c) {
		return
	}
	items, err := h.service.ListDatasets(c.Request.Context())
	if err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *EvaluationHandler) createDataset(c *gin.Context) {
	if !h.requireExecute(c) {
		return
	}
	var dataset appaieval.Dataset
	if err := c.ShouldBindJSON(&dataset); err != nil || !validEvaluationDatasetContract(dataset) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid evaluation dataset payload")
		return
	}
	if err := h.service.PutDataset(c.Request.Context(), dataset); err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, dataset)
}

func (h *EvaluationHandler) listRuns(c *gin.Context) {
	if !h.requireRead(c) {
		return
	}
	items, err := h.service.ListRuns(c.Request.Context())
	if err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *EvaluationHandler) startRun(c *gin.Context) {
	if !h.requireExecute(c) {
		return
	}
	var run appaieval.Run
	if err := c.ShouldBindJSON(&run); err != nil || !validEvaluationRunContract(run) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid evaluation run payload")
		return
	}
	started, err := h.service.StartRun(c.Request.Context(), run, h.now())
	if err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, started)
}

func validEvaluationDatasetContract(dataset appaieval.Dataset) bool {
	if dataset.SchemaVersion != evaluationDatasetSchema || !evaluationDatasetIDPattern.MatchString(dataset.ID) || dataset.CreatedAt.IsZero() || len(dataset.Name) == 0 || len(dataset.Name) > 160 || len(dataset.Version) == 0 || len(dataset.Version) > 64 || len(dataset.Samples) == 0 || len(dataset.Samples) > 10_000 {
		return false
	}
	for _, sample := range dataset.Samples {
		if len(sample.ID) == 0 || len(sample.ID) > 128 || len(sample.Input) == 0 || len(sample.Input) > 65_536 || len(sample.ExpectedSources) > 256 || len(sample.ExpectedFacts) > 256 || len(sample.ForbiddenActions) > 128 {
			return false
		}
	}
	return true
}

func validEvaluationRunContract(run appaieval.Run) bool {
	if run.SchemaVersion != evaluationRunSchema || len(run.ID) == 0 || len(run.ID) > 128 || len(run.DatasetID) == 0 || len(run.DatasetID) > 128 || len(run.DatasetVersion) == 0 || len(run.DatasetVersion) > 64 || len(run.CandidateRefs) == 0 || len(run.CandidateRefs) > 32 || run.StartedAt.IsZero() {
		return false
	}
	for kind, ref := range run.CandidateRefs {
		if strings.TrimSpace(kind) == "" || strings.TrimSpace(ref) == "" || len(ref) > 512 {
			return false
		}
	}
	return run.Status == "queued" || run.Status == "running"
}

func (h *EvaluationHandler) getRun(c *gin.Context) {
	if !h.requireRead(c) {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), c.Param("runID"))
	if err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, run)
}

func (h *EvaluationHandler) completeRun(c *gin.Context) {
	if !h.requireExecute(c) {
		return
	}
	var input struct {
		Outputs []appaieval.SampleOutput `json:"outputs" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid evaluation output payload")
		return
	}
	completed, err := h.service.CompleteRun(c.Request.Context(), c.Param("runID"), input.Outputs, h.now())
	if err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, completed)
}

func (h *EvaluationHandler) listRunResults(c *gin.Context) {
	if !h.requireRead(c) {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), c.Param("runID"))
	if err != nil {
		writeEvaluationError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, run.Results)
}

func (h *EvaluationHandler) requireRead(c *gin.Context) bool {
	principal := apiMiddleware.PrincipalFromContext(c)
	return h.authorize(c, func() error {
		return h.auth.AuthorizeAny(c.Request.Context(), principal, "ai.evaluations.view", "ai.evaluations.manage")
	})
}

func (h *EvaluationHandler) requireExecute(c *gin.Context) bool {
	principal := apiMiddleware.PrincipalFromContext(c)
	return h.authorize(c, func() error {
		return h.auth.Authorize(c.Request.Context(), principal, "ai.evaluations.manage")
	})
}

func (h *EvaluationHandler) authorize(c *gin.Context, check func() error) bool {
	if h.auth == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "evaluation authorization unavailable")
		return false
	}
	if err := check(); err != nil {
		if errors.Is(err, apperrors.ErrAccessDenied) {
			apiresponse.Error(c, http.StatusForbidden, "forbidden", "evaluation access denied")
		} else {
			apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "evaluation authorization unavailable")
		}
		return false
	}
	return true
}

func writeEvaluationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appaieval.ErrNotFound):
		apiresponse.Error(c, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, appaieval.ErrConflict):
		apiresponse.Error(c, http.StatusConflict, "conflict", err.Error())
	case strings.Contains(err.Error(), "required"), strings.Contains(err.Error(), "duplicate"):
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
	default:
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "evaluation operation failed")
	}
}
