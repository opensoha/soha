package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type VirtualizationBindingService interface {
	GetVMAccessBinding(context.Context, domainidentity.Principal, string) (sohaapi.VirtualMachineAccessBinding, error)
	SaveVMAccessBinding(context.Context, domainidentity.Principal, string, sohaapi.VirtualMachineAccessBindingInput) (sohaapi.VirtualMachineAccessBinding, error)
}
type VirtualizationSSHSessionService interface {
	CreateVMSSHSession(context.Context, domainidentity.Principal, string, sohaapi.VirtualMachineSSHSessionInput) (sohaapi.VirtualMachineSSHSession, error)
	GetVMSSHSession(context.Context, domainidentity.Principal, string) (sohaapi.VirtualMachineSSHSession, error)
	CloseVMSSHSession(context.Context, domainidentity.Principal, string) (sohaapi.VirtualMachineSSHSession, error)
	OpenVMSSHStream(context.Context, domainidentity.Principal, string) (appvirtualization.SSHChannel, error)
	RedeemVMSSHCredentials(context.Context, string, string, sohaapi.VMSSHCredentialRedemptionInput) (sohaapi.SecretLeaseRedemption, error)
}
type VirtualizationSSHCommandService interface {
	CreateVMSSHCommand(context.Context, domainidentity.Principal, string, sohaapi.VirtualMachineSSHCommandInput) (sohaapi.VirtualMachineSSHCommand, error)
	GetVMSSHCommand(context.Context, domainidentity.Principal, string) (sohaapi.VirtualMachineSSHCommand, error)
	CancelVMSSHCommand(context.Context, domainidentity.Principal, string) (sohaapi.VirtualMachineSSHCommand, error)
	CheckVMSSHCommandAuthorization(context.Context, string, string, sohaapi.VMSSHCredentialRedemptionInput) (sohaapi.VirtualMachineSSHCommand, error)
}
type VirtualizationGuestAccessService interface {
	VirtualizationBindingService
	VirtualizationSSHSessionService
	VirtualizationSSHCommandService
}

func (h *VirtualizationHandler) guestAccessAvailable(c *gin.Context, available bool) bool {
	if available {
		return true
	}
	apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "VM guest access is unavailable")
	return false
}

func decodeVMAccessJSON(c *gin.Context, input any, required ...string) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	var fields map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(raw, &fields)
	}
	if err == nil {
		for _, name := range required {
			value, ok := fields[name]
			if !ok || string(value) == "null" {
				err = io.ErrUnexpectedEOF
				break
			}
		}
	}
	if err == nil {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(input)
	}
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "complete VM access input is required")
		return false
	}
	return true
}

func (h *VirtualizationHandler) GetVMAccessBinding(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.vmBindings != nil) {
		return
	}
	item, err := h.vmBindings.GetVMAccessBinding(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *VirtualizationHandler) SaveVMAccessBinding(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.vmBindings != nil) {
		return
	}
	var input sohaapi.VirtualMachineAccessBindingInput
	if !decodeVMAccessJSON(c, &input, "expectedRevision", "username", "address", "port", "credentialRef", "executionClusterId", "executionAgentId", "trustedHostKey") {
		return
	}
	item, err := h.vmBindings.SaveVMAccessBinding(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *VirtualizationHandler) CreateVMSSHSession(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshSessions != nil) {
		return
	}
	input := sohaapi.VirtualMachineSSHSessionInput{Cols: 80, Rows: 24}
	if !decodeVMAccessJSON(c, &input, "bindingRevision") {
		return
	}
	ctx := appvirtualization.WithSSHBrowserSession(c.Request.Context(), apiMiddleware.AccessContextFromContext(c).SessionID)
	item, err := h.sshSessions.CreateVMSSHSession(ctx, apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *VirtualizationHandler) GetVMSSHSession(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshSessions != nil) {
		return
	}
	item, err := h.sshSessions.GetVMSSHSession(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *VirtualizationHandler) CloseVMSSHSession(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshSessions != nil) {
		return
	}
	item, err := h.sshSessions.CloseVMSSHSession(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *VirtualizationHandler) RedeemVMSSHCredentials(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshSessions != nil) {
		return
	}
	var input sohaapi.VMSSHCredentialRedemptionInput
	if !decodeVMAccessJSON(c, &input, "clusterId", "agentId", "leaseId", "token") {
		return
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	item, err := h.sshSessions.RedeemVMSSHCredentials(c.Request.Context(), c.Param("id"), token, input)
	if err != nil {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "SSH lease is unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *VirtualizationHandler) CreateVMSSHCommand(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshCommands != nil) {
		return
	}
	input := sohaapi.VirtualMachineSSHCommandInput{TimeoutSeconds: 60}
	if !decodeVMAccessJSON(c, &input, "bindingRevision", "command", "idempotencyKey") {
		return
	}
	ctx := appvirtualization.WithSSHBrowserSession(c.Request.Context(), apiMiddleware.AccessContextFromContext(c).SessionID)
	item, err := h.sshCommands.CreateVMSSHCommand(ctx, apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *VirtualizationHandler) GetVMSSHCommand(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshCommands != nil) {
		return
	}
	item, err := h.sshCommands.GetVMSSHCommand(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *VirtualizationHandler) CancelVMSSHCommand(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshCommands != nil) {
		return
	}
	item, err := h.sshCommands.CancelVMSSHCommand(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *VirtualizationHandler) CheckVMSSHCommandAuthorization(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshCommands != nil) {
		return
	}
	var input sohaapi.VMSSHCredentialRedemptionInput
	if !decodeVMAccessJSON(c, &input, "clusterId", "agentId", "leaseId", "token") {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	item, err := h.sshCommands.CheckVMSSHCommandAuthorization(ctx, c.Param("id"), strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "), input)
	if err != nil {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "SSH command authorization unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *VirtualizationHandler) StreamVMSSHSession(c *gin.Context) {
	if !h.guestAccessAvailable(c, h.sshSessions != nil) {
		return
	}
	if !allowWebSocketOrigin(c.Request) {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "WebSocket origin denied")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	defer cancel()
	channel, err := h.sshSessions.OpenVMSSHStream(ctx, apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	defer func() { _ = channel.Close() }()
	conn, err := podTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(128 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	done := make(chan struct{}, 1)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			var frame sohaapi.VirtualMachineSSHFrame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			if !validVMSSHInputFrame(frame) {
				return
			}
			if frame.Type == "close" {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
			if err := channel.WriteFrame(frame); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := channel.ReadFrame()
			if err != nil {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err = conn.WriteJSON(frame); err != nil || frame.Type == "close" {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	_ = conn.Close()
	_ = channel.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

func validVMSSHInputFrame(frame sohaapi.VirtualMachineSSHFrame) bool {
	if frame.Message != "" || frame.ExitCode != nil {
		return false
	}
	switch frame.Type {
	case "input":
		if frame.Cols != 0 || frame.Rows != 0 {
			return false
		}
		if frame.DataEncoding == "base64" {
			data, err := base64.StdEncoding.DecodeString(frame.Data)
			return err == nil && len(data) <= 65536
		}
		return (frame.DataEncoding == "" || frame.DataEncoding == "utf8") && len(frame.Data) <= 65536
	case "resize":
		return frame.Cols > 0 && frame.Cols <= 512 && frame.Rows > 0 && frame.Rows <= 512 && emptySSHFrameData(frame)
	case "close":
		return emptySSHFrameData(frame) && frame.Cols == 0 && frame.Rows == 0
	default:
		return false
	}
}

func emptySSHFrameData(frame sohaapi.VirtualMachineSSHFrame) bool {
	return frame.Data == "" && frame.DataEncoding == ""
}
