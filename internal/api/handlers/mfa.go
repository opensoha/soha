package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type MFAService interface {
	ListCredentials(context.Context, domainidentity.Principal) ([]sohaapi.MFACredential, error)
	RevokeCredential(context.Context, domainidentity.Principal, string, string) error
	BeginTOTPEnrollment(context.Context, domainidentity.Principal, string) (sohaapi.MFAEnrollmentChallenge, error)
	BeginRecoveryChallenge(context.Context, domainidentity.Principal, string) (sohaapi.MFARecoveryChallenge, error)
	VerifyChallenge(context.Context, domainidentity.Principal, string, string, string) (sohaapi.MFAChallengeResult, error)
	BeginWebAuthnEnrollment(context.Context, domainidentity.Principal, string) (sohaapi.MFAWebAuthnCreationOptions, error)
	BeginWebAuthnAuthentication(context.Context, domainidentity.Principal, string, sohaapi.MFAWebAuthnAuthenticationRequest) (sohaapi.MFAWebAuthnRequestOptions, error)
	VerifyWebAuthnChallenge(context.Context, domainidentity.Principal, string, string, sohaapi.MFAWebAuthnResponse) (sohaapi.MFAChallengeResult, error)
	RegenerateRecoveryCodes(context.Context, domainidentity.Principal, string) (sohaapi.MFARecoveryCodeSet, error)
	AdminRevokeCredential(context.Context, domainidentity.Principal, string, string) (sohaapi.OperationStatus, error)
	AdminResetUserMFA(context.Context, domainidentity.Principal, string, sohaapi.MFAAdminResetRequest) (sohaapi.MFAAdminResetResult, error)
}

type MFAHandler struct {
	service MFAService
}

func NewMFAHandler(service MFAService) *MFAHandler { return &MFAHandler{service: service} }

func (h *MFAHandler) ListCredentials(c *gin.Context) {
	items, err := h.service.ListCredentials(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MFAHandler) RevokeCredential(c *gin.Context) {
	if err := h.service.RevokeCredential(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c), c.Param("mfaCredentialID")); err != nil {
		writeMFAError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *MFAHandler) BeginTOTPEnrollment(c *gin.Context) {
	item, err := h.service.BeginTOTPEnrollment(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c))
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MFAHandler) BeginRecoveryChallenge(c *gin.Context) {
	item, err := h.service.BeginRecoveryChallenge(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c))
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MFAHandler) VerifyChallenge(c *gin.Context) {
	var request sohaapi.MFAChallengeVerifyRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(strings.TrimSpace(request.Response)) > 128 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid MFA response")
		return
	}
	item, err := h.service.VerifyChallenge(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c), c.Param("mfaChallengeID"), request.Response)
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MFAHandler) BeginWebAuthnEnrollment(c *gin.Context) {
	item, err := h.service.BeginWebAuthnEnrollment(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c))
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MFAHandler) BeginWebAuthnAuthentication(c *gin.Context) {
	var request sohaapi.MFAWebAuthnAuthenticationRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.Purpose.Valid() {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid WebAuthn authentication request")
		return
	}
	item, err := h.service.BeginWebAuthnAuthentication(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c), request)
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MFAHandler) VerifyWebAuthnChallenge(c *gin.Context) {
	var request sohaapi.MFAWebAuthnResponse
	if err := c.ShouldBindJSON(&request); err != nil || !validWebAuthnResponse(request) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid WebAuthn response")
		return
	}
	item, err := h.service.VerifyWebAuthnChallenge(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c), c.Param("mfaChallengeID"), request)
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MFAHandler) RegenerateRecoveryCodes(c *gin.Context) {
	item, err := h.service.RegenerateRecoveryCodes(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), sessionID(c))
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MFAHandler) AdminRevokeCredential(c *gin.Context) {
	item, err := h.service.AdminRevokeCredential(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("identityUserID"), c.Param("mfaCredentialID"))
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MFAHandler) AdminResetUserMFA(c *gin.Context) {
	var request sohaapi.MFAAdminResetRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Reason) == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid MFA reset payload")
		return
	}
	item, err := h.service.AdminResetUserMFA(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("identityUserID"), request)
	if err != nil {
		writeMFAError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func validWebAuthnResponse(request sohaapi.MFAWebAuthnResponse) bool {
	base := strings.TrimSpace(request.CredentialID) != "" && len(request.CredentialID) <= 2048 &&
		strings.TrimSpace(request.ClientDataJSON) != "" && len(request.ClientDataJSON) <= 65536
	registration := strings.TrimSpace(request.AttestationObject) != "" && len(request.AttestationObject) <= 131072
	authentication := strings.TrimSpace(request.AuthenticatorData) != "" && len(request.AuthenticatorData) <= 65536 &&
		strings.TrimSpace(request.Signature) != "" && len(request.Signature) <= 65536 && len(request.UserHandle) <= 2048
	return base && (registration || authentication)
}

func sessionID(c *gin.Context) string {
	return strings.TrimSpace(apiMiddleware.AccessContextFromContext(c).SessionID)
}

func writeMFAError(c *gin.Context, err error) {
	_ = c.Error(err)
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied):
		status, code, message = http.StatusForbidden, "forbidden", "permission denied"
	case errors.Is(err, apperrors.ErrUnauthorized):
		status, code, message = http.StatusUnauthorized, "mfa_verification_failed", "MFA verification failed"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		status, code, message = http.StatusBadRequest, "invalid_argument", "invalid MFA request"
	case errors.Is(err, apperrors.ErrConflict):
		status, code, message = http.StatusConflict, "mfa_challenge_conflict", "MFA challenge is expired, locked, or already consumed"
	case errors.Is(err, apperrors.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "MFA credential not found"
	}
	apiresponse.Error(c, status, code, message)
}
