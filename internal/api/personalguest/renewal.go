package personalguest

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
)

type renewalService interface {
	Renew(context.Context, networkidentity.Identity, guest.Renewal) (guest.RenewalResult, error)
	CommitRenewal(context.Context, networkidentity.Identity, string) (guest.Binding, error)
}

func (h *handler) renew(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(renewalService)
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input guest.Renewal
	if !readBody(c, &input) {
		return
	}
	result, err := s.Renew(c.Request.Context(), identity, input)
	if errors.Is(err, domain.ErrPersonalGuestRenewalExpired) {
		apiresponse.Error(c, http.StatusGone, "personal_guest_renewal_expired", "pending renewal expired; verify current binding before issuing another request")
		return
	}
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) commitRenewal(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(renewalService)
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	if c.Request.ContentLength != 0 {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := s.CommitRenewal(c.Request.Context(), identity, c.Param("requestID"))
	if errors.Is(err, domain.ErrPersonalGuestRenewalExpired) {
		apiresponse.Error(c, http.StatusGone, "personal_guest_renewal_expired", "pending renewal expired; verify current binding before issuing another request")
		return
	}
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}
