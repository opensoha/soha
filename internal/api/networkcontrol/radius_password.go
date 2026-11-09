package networkcontrol

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type RadiusPasswordRuntime interface {
	VerifyNASPassword(context.Context, networkidentity.Identity, string, []byte) (networkprotocol.RadiusPasswordResult, error)
}

// This boundary deliberately avoids ordinary JSON error writers. rlm_rest
// consumes only known RADIUS attributes, and no rejection echoes input data.
func (h *handler) radiusVerifyPassword(c *gin.Context) {
	identity, err := authenticatedIdentity(c.Request)
	if err != nil || identity.Kind != "nas" || identity.ID != c.Param("runtimeID") {
		c.Status(http.StatusUnauthorized)
		return
	}
	if h.options.PasswordRuntime == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	address, err := netip.ParseAddr(host)
	if err != nil || !h.passwordPeerAllowed(address.Unmap()) {
		c.Status(http.StatusUnauthorized)
		return
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" || c.GetHeader("Content-Encoding") != "" && !strings.EqualFold(c.GetHeader("Content-Encoding"), "identity") {
		c.Status(http.StatusUnauthorized)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	defer clear(raw)
	result, err := h.options.PasswordRuntime.VerifyNASPassword(c.Request.Context(), identity, address.String(), raw)
	if err != nil {
		switch {
		case errors.Is(err, apperrors.ErrConflict):
			c.Status(http.StatusTooManyRequests)
		case errors.Is(err, apperrors.ErrUnauthorized), errors.Is(err, apperrors.ErrAccessDenied), errors.Is(err, apperrors.ErrInvalidArgument):
			c.Status(http.StatusUnauthorized)
		default:
			c.Status(http.StatusServiceUnavailable)
		}
		return
	}
	attribute := func(value string) radiusReplyAttribute {
		return radiusReplyAttribute{DoXLAT: false, Op: ":=", Value: []string{value}}
	}
	c.JSON(http.StatusOK, map[string]radiusReplyAttribute{"control:Tmp-String-4": attribute(result.AuthenticationReceipt), "control:Tmp-String-5": attribute(result.SubjectID)})
}

func (h *handler) passwordPeerAllowed(address netip.Addr) bool {
	for _, raw := range h.options.PasswordAllowedPeers {
		if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}
