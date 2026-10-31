package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerVersionedRuntimeService interface {
	ReadProjectRuntime(context.Context, domainidentity.Principal, string, appdocker.RuntimeReadInput) (protocol.ReadResult, error)
}

func dockerVersionedRuntimeMissing(service DockerVersionedRuntimeService) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func (h *DockerHandler) GetRuntimeLogsV1(c *gin.Context)    { h.readRuntimeV1(c, "logs") }
func (h *DockerHandler) GetRuntimeVolumesV1(c *gin.Context) { h.readRuntimeV1(c, "volumes") }
func (h *DockerHandler) ListRuntimeFilesV1(c *gin.Context)  { h.readRuntimeV1(c, "files.list") }
func (h *DockerHandler) ReadRuntimeFileV1(c *gin.Context)   { h.readRuntimeV1(c, "files.read") }

func (h *DockerHandler) readRuntimeV1(c *gin.Context, action string) {
	if h.versionedRuntime == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	if len(c.Request.URL.RawQuery) > 16<<10 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	input, err := dockerReadQuery(query, action)
	if err != nil {
		writeError(c, err)
		return
	}
	if action == "files.list" || action == "files.read" {
		err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(75 * time.Second))
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			writeError(c, apperrors.ErrClusterUnready)
			return
		}
	}
	result, err := h.versionedRuntime.ReadProjectRuntime(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func dockerReadQuery(query url.Values, action string) (appdocker.RuntimeReadInput, error) {
	input := appdocker.RuntimeReadInput{Action: action}
	allowed := map[string]bool{"serviceName": true}
	switch action {
	case "logs":
		allowed["tailLines"], allowed["sinceSeconds"], allowed["limitBytes"] = true, true, true
	case "volumes":
	case "files.list":
		allowed["target"], allowed["path"], allowed["limit"] = true, true, true
	case "files.read":
		allowed["target"], allowed["path"], allowed["limitBytes"] = true, true, true
	default:
		return input, apperrors.ErrInvalidArgument
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return input, apperrors.ErrInvalidArgument
		}
	}
	input.ServiceName, input.Target, input.Path = query.Get("serviceName"), query.Get("target"), query.Get("path")
	if input.ServiceName == "" {
		return input, apperrors.ErrInvalidArgument
	}
	if err := dockerReadIntegerQuery(query, &input); err != nil {
		return input, err
	}
	if !validDockerReadLimits(input) {
		return input, apperrors.ErrInvalidArgument
	}
	return input, nil
}

func validDockerReadLimits(input appdocker.RuntimeReadInput) bool {
	return input.TailLines >= 0 && input.TailLines <= 2000 && input.SinceSeconds >= 0 && input.SinceSeconds <= 604800 && input.Limit >= 0 && input.Limit <= 1000 && input.LimitBytes >= 0 && input.LimitBytes <= 1<<20
}

func dockerReadIntegerQuery(query url.Values, input *appdocker.RuntimeReadInput) error {
	values := map[string]*int64{"sinceSeconds": &input.SinceSeconds, "limitBytes": &input.LimitBytes}
	for name, pointer := range values {
		if raw, ok := query[name]; ok {
			number, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil {
				return apperrors.ErrInvalidArgument
			}
			*pointer = number
		}
	}
	for name, pointer := range map[string]*int{"tailLines": &input.TailLines, "limit": &input.Limit} {
		if raw, ok := query[name]; ok {
			number, err := strconv.Atoi(raw[0])
			if err != nil {
				return apperrors.ErrInvalidArgument
			}
			*pointer = number
		}
	}
	return nil
}
