package networkcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
)

type ProxyDeploymentRuntime interface {
	RuntimeControl(context.Context, networkidentity.Identity, string, string) (api.NetworkProxyDeploymentRuntimeControl, error)
	RuntimeACK(context.Context, networkidentity.Identity, api.NetworkProxyDeploymentRuntimeAck) error
}

func (h *handler) proxyDeploymentControl(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	instance, epoch := c.Request.URL.Query()["instanceId"], c.Request.URL.Query()["processEpoch"]
	if len(instance) != 1 || len(epoch) != 1 || instance[0] == "" || epoch[0] == "" {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	control, err := h.options.ProxyDeployment.RuntimeControl(c.Request.Context(), identity, instance[0], epoch[0])
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, control)
}
func (h *handler) proxyDeploymentACK(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	var ack api.NetworkProxyDeploymentRuntimeAck
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ack); err != nil {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	if !proxyDeploymentACKFieldsPresent(raw, ack) {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	if err := h.options.ProxyDeployment.RuntimeACK(c.Request.Context(), identity, ack); err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, ack)
}

func proxyDeploymentACKFieldsPresent(raw []byte, ack api.NetworkProxyDeploymentRuntimeAck) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	required := []string{"runtimeId", "instanceId", "processEpoch", "credentialId", "credentialEpoch", "ownerEpoch", "controlRevision", "expiresAt", "observedAt", "acceptingRequests", "activeCountKnown"}
	if ack.ActiveCountKnown || ack.ProcessStopped {
		required = append(required, "activeConnections")
	}
	for _, name := range required {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return !ack.ProcessStopped || (!ack.AcceptingRequests && ack.ActiveCountKnown && ack.ActiveConnections == 0)
}
