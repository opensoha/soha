package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workflowRequestService interface {
	SubmitWorkflowRequest(context.Context, domainidentity.Principal, string, domainworkflow.Input) (domainworkflow.TriggerReceipt, error)
	GetWorkflowRequest(context.Context, domainidentity.Principal, string) (domainworkflow.TriggerReceipt, error)
}

func (h *WorkflowHandler) SubmitRequest(c *gin.Context) {
	service, ok := h.service.(workflowRequestService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	input, err := decodeWorkflowRequest(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow request")
		return
	}
	request := input.Workflow
	variables, err := workflowRequestValues(request.Variables)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow parameter values")
		return
	}
	buildArgs, err := workflowRequestValues(request.BuildArgs)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow parameter values")
		return
	}
	item, err := service.SubmitWorkflowRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.IdempotencyKey, domainworkflow.Input{
		ApplicationID: request.ApplicationID, ApplicationEnvironmentID: request.ApplicationEnvironmentID, WorkflowName: request.WorkflowName,
		ClusterID: request.ClusterID, Namespace: request.Namespace, DeploymentName: request.DeploymentName, BuildSourceID: request.BuildSourceID,
		RefType: request.RefType, RefName: request.RefName, ImageTag: request.ImageTag, ReleaseName: request.ReleaseName, ContainerName: request.ContainerName,
		Variables: variables, BuildArgs: buildArgs, TriggerBuild: request.TriggerBuild, TriggerRelease: request.TriggerRelease,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *WorkflowHandler) GetRequest(c *gin.Context) {
	service, ok := h.service.(workflowRequestService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := service.GetWorkflowRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("requestKey"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func decodeWorkflowRequest(c *gin.Context) (sohaapi.WorkflowRequestInput, error) {
	var input sohaapi.WorkflowRequestInput
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 || !utf8.Valid(body) {
		return input, apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return input, apperrors.ErrInvalidArgument
	}
	if input.IdempotencyKey == "" || input.Workflow.ApplicationID == "" {
		return input, apperrors.ErrInvalidArgument
	}
	return input, nil
}

func workflowRequestValues(values *sohaapi.TemplateParameterValues) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
