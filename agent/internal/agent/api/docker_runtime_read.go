package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/agent/internal/agent/dockerruntime"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

type dockerVersionedReader func(context.Context, protocol.ReadRequest) (protocol.ReadResult, error)

func registerDockerVersionedReadRoutes(group *gin.RouterGroup, actions actionPolicy, reader *dockerruntime.VolumeReader) {
	group.POST("/v1/logs", actions.Require(actionDockerVersionedLogs), dockerVersionedRead("logs", dockerruntime.ReadLogs))
	group.POST("/v1/volumes", actions.Require(actionDockerVersionedFiles), dockerVersionedRead("volumes", reader.ReadVolumes))
	group.POST("/v1/files/list", actions.Require(actionDockerVersionedFiles), dockerVersionedRead("files.list", reader.ReadFiles))
	group.POST("/v1/files/read", actions.Require(actionDockerVersionedFiles), dockerVersionedRead("files.read", reader.ReadFiles))
}

func dockerVersionedRead(action string, read dockerVersionedReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		raw, bodyErr := io.ReadAll(c.Request.Body)
		if bodyErr != nil || !utf8.Valid(raw) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "invalid versioned Docker read request")
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var request protocol.ReadRequest
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "invalid versioned Docker read request")
			return
		}
		req, err := request.Normalized(action)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "invalid versioned Docker read request")
			return
		}
		if (action == "files.list" || action == "files.read") && setDockerReadDeadline(c) != nil {
			apiresponse.Error(c, http.StatusInternalServerError, "runtime_read_unavailable", "bounded Docker read response is unavailable")
			return
		}
		result, err := read(c.Request.Context(), req)
		if errors.Is(err, dockerruntime.ErrVolumeReaderUnsupported) {
			apiresponse.Error(c, http.StatusNotImplemented, "runtime_read_unsupported", "safe Docker volume reading is unavailable for this target")
			return
		}
		if err != nil || result.ValidateFor(req) != nil {
			apiresponse.Error(c, http.StatusBadGateway, "runtime_read_failed", "Docker runtime read could not be confirmed")
			return
		}
		apiresponse.Item(c, http.StatusOK, result)
	}
}

func setDockerReadDeadline(c *gin.Context) error {
	err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(65 * time.Second))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
