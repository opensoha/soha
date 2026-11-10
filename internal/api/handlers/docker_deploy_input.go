package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
)

// Preserve the legacy empty-body default, but never turn malformed or partial
// JSON into an operation with a default (or partially decoded) action.
func decodeDockerProjectDeploy(c *gin.Context) (domaindocker.ProjectDeployInput, bool) {
	var input domaindocker.ProjectDeployInput
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		apiresponse.Error(c, http.StatusRequestEntityTooLarge, "payload_too_large", "docker deploy payload exceeds 4 KiB")
		return input, false
	}
	if err == nil && len(bytes.TrimSpace(raw)) == 0 {
		return input, true
	}
	var req sohaapi.DockerProjectDeployInput
	if err == nil && utf8.Valid(raw) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&req)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil || !dockerProjectDeployValid(raw, req) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker deploy payload or action")
		return input, false
	}
	input.Action, input.Preconditions = string(req.Action), req.Preconditions
	return input, true
}

func dockerProjectDeployValid(raw []byte, req sohaapi.DockerProjectDeployInput) bool {
	if !utf8.Valid(raw) {
		return false
	}
	keys := json.NewDecoder(bytes.NewReader(raw))
	if validateConfigurationJSON(keys, 0) != nil || keys.Decode(new(any)) != io.EOF || validateConfigurationTypedKeys(raw, reflect.TypeFor[sohaapi.DockerProjectDeployInput]()) != nil {
		return false
	}
	if value := req.Preconditions; value != nil {
		if value.HostID == "" || len(value.HostID) > 128 || strings.TrimSpace(value.HostID) != value.HostID || value.Host.ExpectedUID == uuid.Nil || value.Host.ExpectedConfigurationRevision == uuid.Nil || value.Project.ExpectedUID == uuid.Nil || value.Project.ExpectedConfigurationRevision == uuid.Nil {
			return false
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	// encoding/json matches struct fields without regard to case. The public
	// schema's property names are exact; do not let aliases bypass validation.
	for name := range fields {
		if name != "action" && name != "preconditions" {
			return false
		}
	}
	if _, present := fields["action"]; present {
		return !bytes.Equal(bytes.TrimSpace(fields["action"]), []byte("null")) && req.Action.Valid()
	}
	return true
}
