package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *VirtualizationHandler) ListWorkerPools(c *gin.Context) {
	if h.workerPools == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "worker pools are unavailable")
		return
	}
	items, err := h.workerPools.ListWorkerPools(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("connectionId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *VirtualizationHandler) GetWorkerPool(c *gin.Context) {
	if h.workerPools == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "worker pools are unavailable")
		return
	}
	pool, err := h.workerPools.GetWorkerPool(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, pool)
}

func (h *VirtualizationHandler) SaveWorkerPool(c *gin.Context) {
	if h.workerPools == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "worker pools are unavailable")
		return
	}
	var request struct {
		Spec             *sohaapi.VirtualizationWorkerPoolSpec `json:"spec"`
		ExpectedRevision *int                                  `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Spec == nil || request.ExpectedRevision == nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid worker pool request")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "one worker pool request is required")
		return
	}
	input := sohaapi.VirtualizationWorkerPoolInput{Spec: *request.Spec, ExpectedRevision: *request.ExpectedRevision}
	pool, err := h.workerPools.SaveWorkerPool(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, pool)
}

func (h *VirtualizationHandler) DeleteWorkerPool(c *gin.Context) {
	if h.workerPools == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "worker pools are unavailable")
		return
	}
	revision, err := strconv.Atoi(c.Query("expectedRevision"))
	if err != nil || revision < 1 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expectedRevision must be positive")
		return
	}
	if err := h.workerPools.DeleteWorkerPool(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), revision); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *VirtualizationHandler) CreateWorker(c *gin.Context) {
	if h.workerPools == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "worker supply is unavailable")
		return
	}
	var request sohaapi.VirtualizationWorkerCreateInput
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid worker creation request")
		return
	}
	task, err := h.workerPools.CreateWorker(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) AssessWorkerReadiness(c *gin.Context) {
	if h.workerPools == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "worker observations are unavailable")
		return
	}
	assessment, err := h.workerPools.AssessWorkerReadiness(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, assessment)
}
