package providerportal

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PortalReader interface {
	PortalBootstrap(context.Context, domainidentity.Principal) (domainportal.PortalBootstrap, error)
	ListPortalApplications(context.Context, domainidentity.Principal) ([]domainportal.Application, error)
	GetPortalApplication(context.Context, domainidentity.Principal, string) (domainportal.Application, error)
	SecuritySummary(context.Context, domainidentity.Principal) domainportal.PortalSecuritySummary
}

type PortalInteractor interface {
	Launch(context.Context, domainidentity.Principal, string) (domainportal.LaunchDecision, error)
	SetFavorite(context.Context, domainidentity.Principal, string) (domainportal.Application, error)
	DeleteFavorite(context.Context, domainidentity.Principal, string) error
	ListRecent(context.Context, domainidentity.Principal, int) ([]domainportal.ApplicationLaunch, error)
}

type ApplicationService interface {
	ListApplications(context.Context, domainidentity.Principal, domainportal.ApplicationFilter) ([]domainportal.Application, error)
	GetApplication(context.Context, domainidentity.Principal, string) (domainportal.Application, error)
	CreateApplication(context.Context, domainidentity.Principal, domainportal.ApplicationInput) (domainportal.Application, error)
	UpdateApplication(context.Context, domainidentity.Principal, string, domainportal.ApplicationInput) (domainportal.Application, error)
	DeleteApplication(context.Context, domainidentity.Principal, string) error
	ProviderCapabilities() []domainportal.ProviderCapability
}

type PolicyService interface {
	ListPolicies(context.Context, domainidentity.Principal, domainportal.ApplicationFilter) ([]domainportal.ApplicationPolicy, error)
	GetPolicy(context.Context, domainidentity.Principal, string) (domainportal.ApplicationPolicy, error)
	UpdatePolicy(context.Context, domainidentity.Principal, string, domainportal.ApplicationPolicyInput) (domainportal.ApplicationPolicy, error)
}

type OIDCService interface {
	Discovery(string) domainprovider.DiscoveryDocument
	JWKS(context.Context) (domainprovider.JWKS, error)
	Authorize(context.Context, string, domainidentity.Principal, domainprovider.AuthorizeInput) (domainprovider.AuthorizeResult, error)
	Token(context.Context, string, domainprovider.TokenInput) (domainprovider.TokenResponse, error)
	Introspect(context.Context, string, string, domainprovider.ClientAuthInput) (domainprovider.IntrospectionResponse, error)
	Revoke(context.Context, string, string, domainprovider.ClientAuthInput) error
	UserInfo(context.Context, string, string) (domainprovider.UserInfoResponse, error)
}

type ProxyService interface {
	ProxyAuth(context.Context, domainidentity.Principal, domainprovider.ProxyAuthInput) (domainprovider.ProxyAuthResult, error)
	ProxyCookieDomain(context.Context, domainprovider.ProxyAuthInput) (string, error)
	IssueProxySession(context.Context, domainidentity.Principal) (domainprovider.ProxySession, error)
}

type ProviderService interface {
	ListProviders(context.Context, domainidentity.Principal, domainprovider.ProviderFilter) ([]domainprovider.Provider, error)
	GetProvider(context.Context, domainidentity.Principal, string) (domainprovider.Provider, error)
	CreateProvider(context.Context, domainidentity.Principal, domainprovider.ProviderInput) (domainprovider.Provider, error)
	UpdateProvider(context.Context, domainidentity.Principal, string, domainprovider.ProviderInput) (domainprovider.Provider, error)
	DeleteProvider(context.Context, domainidentity.Principal, string) error
}

type OutpostService interface {
	ListOutposts(context.Context, domainidentity.Principal, domainprovider.OutpostFilter) ([]domainprovider.Outpost, error)
	GetOutpost(context.Context, domainidentity.Principal, string) (domainprovider.Outpost, error)
	CreateOutpost(context.Context, domainidentity.Principal, domainprovider.OutpostInput) (domainprovider.Outpost, error)
	UpdateOutpost(context.Context, domainidentity.Principal, string, domainprovider.OutpostInput) (domainprovider.Outpost, error)
	DeleteOutpost(context.Context, domainidentity.Principal, string) error
}

type OutpostRuntimeService interface {
	ClaimOutpost(context.Context, domainprovider.OutpostClaimInput) (domainprovider.OutpostClaimResult, error)
	HeartbeatOutpost(context.Context, string, domainprovider.OutpostHeartbeatInput) (domainprovider.OutpostHeartbeatResult, error)
	CheckOutpost(context.Context, string, domainprovider.OutpostCheckInput) (domainprovider.ProxyAuthResult, error)
	RecordOutpostEvents(context.Context, string, domainprovider.OutpostEventsInput) (domainprovider.OutpostEventsResult, error)
}

type OIDCClientService interface {
	ListOIDCClients(context.Context, domainidentity.Principal, string) ([]domainprovider.OIDCClient, error)
	CreateOIDCClient(context.Context, domainidentity.Principal, string, domainprovider.OIDCClientInput) (domainprovider.OIDCClientCreated, error)
	UpdateOIDCClient(context.Context, domainidentity.Principal, string, domainprovider.OIDCClientInput) (domainprovider.OIDCClient, error)
	DeleteOIDCClient(context.Context, domainidentity.Principal, string) error
}

type Services struct {
	PortalReader     PortalReader
	PortalInteractor PortalInteractor
	Applications     ApplicationService
	Policies         PolicyService
	Providers        ProviderService
	Outposts         OutpostService
	OIDCClients      OIDCClientService
	OIDC             OIDCService
	Proxy            ProxyService
	OutpostRuntime   OutpostRuntimeService
}

type Handler struct {
	portalHandler
	applicationHandler
	policyHandler
	providerHandler
	outpostHandler
	oidcClientHandler
	oidcHandler
	proxyHandler
	outpostRuntimeHandler
}

type portalHandler struct {
	reader     PortalReader
	interactor PortalInteractor
}

type applicationHandler struct {
	service ApplicationService
}

type policyHandler struct {
	service PolicyService
}

type providerHandler struct {
	service ProviderService
}

type outpostHandler struct {
	service OutpostService
}

type oidcClientHandler struct {
	service OIDCClientService
}

type oidcHandler struct {
	service OIDCService
}

type proxyHandler struct {
	service ProxyService
}

type outpostRuntimeHandler struct {
	service OutpostRuntimeService
}

const proxySessionCookieName = "soha_proxy_session"

func New(services Services) *Handler {
	return &Handler{
		portalHandler: portalHandler{
			reader:     services.PortalReader,
			interactor: services.PortalInteractor,
		},
		applicationHandler:    applicationHandler{service: services.Applications},
		policyHandler:         policyHandler{service: services.Policies},
		providerHandler:       providerHandler{service: services.Providers},
		outpostHandler:        outpostHandler{service: services.Outposts},
		oidcClientHandler:     oidcClientHandler{service: services.OIDCClients},
		oidcHandler:           oidcHandler{service: services.OIDC},
		proxyHandler:          proxyHandler{service: services.Proxy},
		outpostRuntimeHandler: outpostRuntimeHandler{service: services.OutpostRuntime},
	}
}

func (h *portalHandler) PortalBootstrap(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.reader.PortalBootstrap(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *portalHandler) ListPortalApplications(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.reader.ListPortalApplications(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *portalHandler) GetPortalApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.reader.GetPortalApplication(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *portalHandler) LaunchPortalApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.interactor.Launch(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *portalHandler) SetFavorite(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.interactor.SetFavorite(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *portalHandler) DeleteFavorite(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.interactor.DeleteFavorite(c.Request.Context(), principal, c.Param("applicationID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *portalHandler) ListRecent(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.interactor.ListRecent(c.Request.Context(), principal, parseLimit(c.Query("limit"), 10))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *portalHandler) SecuritySummary(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	apiresponse.Item(c, http.StatusOK, h.reader.SecuritySummary(c.Request.Context(), principal))
}

func (h *applicationHandler) ListIdentityApplications(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListApplications(c.Request.Context(), principal, domainportal.ApplicationFilter{
		Query:  c.Query("q"),
		Status: c.Query("status"),
		Limit:  parseLimit(c.Query("limit"), 0),
		Offset: parseOffset(c.Query("offset")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *applicationHandler) GetIdentityApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplication(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *applicationHandler) CreateIdentityApplication(c *gin.Context) {
	var req domainportal.ApplicationInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid identity application payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateApplication(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *applicationHandler) UpdateIdentityApplication(c *gin.Context) {
	var req domainportal.ApplicationInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid identity application payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateApplication(c.Request.Context(), principal, c.Param("applicationID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *applicationHandler) DeleteIdentityApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteApplication(c.Request.Context(), principal, c.Param("applicationID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *policyHandler) ListIdentityPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListPolicies(c.Request.Context(), principal, domainportal.ApplicationFilter{
		Query:  c.Query("q"),
		Status: c.Query("status"),
		Limit:  parseLimit(c.Query("limit"), 0),
		Offset: parseOffset(c.Query("offset")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *policyHandler) GetIdentityPolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetPolicy(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *policyHandler) UpdateIdentityPolicy(c *gin.Context) {
	var req domainportal.ApplicationPolicyInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid identity policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdatePolicy(c.Request.Context(), principal, c.Param("applicationID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *applicationHandler) ProviderCapabilities(c *gin.Context) {
	apiresponse.Items(c, http.StatusOK, h.service.ProviderCapabilities())
}

func (h *providerHandler) ListIdentityProviders(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListProviders(c.Request.Context(), principal, domainprovider.ProviderFilter{
		ApplicationID: c.Query("applicationId"),
		Type:          c.Query("type"),
		Status:        c.Query("status"),
		Limit:         parseLimit(c.Query("limit"), 0),
		Offset:        parseOffset(c.Query("offset")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *providerHandler) GetIdentityProvider(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetProvider(c.Request.Context(), principal, c.Param("providerID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *providerHandler) CreateIdentityProvider(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.ProviderInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid identity provider payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateProvider(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *providerHandler) UpdateIdentityProvider(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.ProviderInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid identity provider payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateProvider(c.Request.Context(), principal, c.Param("providerID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *providerHandler) DeleteIdentityProvider(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteProvider(c.Request.Context(), principal, c.Param("providerID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *outpostHandler) ListOutposts(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOutposts(c.Request.Context(), principal, domainprovider.OutpostFilter{
		Mode:   c.Query("mode"),
		Status: c.Query("status"),
		Limit:  parseLimit(c.Query("limit"), 0),
		Offset: parseOffset(c.Query("offset")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *outpostHandler) GetOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetOutpost(c.Request.Context(), principal, c.Param("outpostID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostHandler) CreateOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OutpostInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateOutpost(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *outpostHandler) UpdateOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OutpostInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOutpost(c.Request.Context(), principal, c.Param("outpostID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostHandler) DeleteOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteOutpost(c.Request.Context(), principal, c.Param("outpostID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *outpostRuntimeHandler) ClaimOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OutpostClaimInput
	if err := bindOptionalJSON(c, &req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost claim payload")
		return
	}
	req.OutpostID = firstNonEmpty(req.OutpostID, c.Query("outpost_id"), c.Query("outpostID"))
	req.Token = outpostTokenFromRequest(c, req.Token)
	item, err := h.service.ClaimOutpost(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostRuntimeHandler) HeartbeatOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OutpostHeartbeatInput
	if err := bindOptionalJSON(c, &req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost heartbeat payload")
		return
	}
	req.Token = outpostTokenFromRequest(c, req.Token)
	item, err := h.service.HeartbeatOutpost(c.Request.Context(), c.Param("outpostID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostRuntimeHandler) CheckOutpost(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OutpostCheckInput
	if err := bindOptionalJSON(c, &req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost check payload")
		return
	}
	req.Token = outpostTokenFromRequest(c, req.Token)
	fillOutpostCheckInputFromHeaders(c, &req)
	item, err := h.service.CheckOutpost(c.Request.Context(), c.Param("outpostID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostRuntimeHandler) OutpostEvents(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OutpostEventsInput
	if err := bindOptionalJSON(c, &req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost events payload")
		return
	}
	req.Token = outpostTokenFromRequest(c, req.Token)
	item, err := h.service.RecordOutpostEvents(c.Request.Context(), c.Param("outpostID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *oidcClientHandler) ListOIDCClients(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOIDCClients(c.Request.Context(), principal, c.Param("providerID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *oidcClientHandler) CreateOIDCClient(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OIDCClientInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oidc client payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateOIDCClient(c.Request.Context(), principal, c.Param("providerID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *oidcClientHandler) UpdateOIDCClient(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	var req domainprovider.OIDCClientInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oidc client payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOIDCClient(c.Request.Context(), principal, c.Param("clientID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *oidcClientHandler) DeleteOIDCClient(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteOIDCClient(c.Request.Context(), principal, c.Param("clientID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *oidcHandler) OIDCDiscovery(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	c.JSON(http.StatusOK, h.service.Discovery(issuerFromRequest(c)))
}

func (h *oidcHandler) OIDCAuthorize(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if strings.TrimSpace(principal.UserID) == "" {
		c.Redirect(http.StatusFound, "/login?return_to="+url.QueryEscape(c.Request.URL.RequestURI()))
		return
	}
	result, err := h.service.Authorize(c.Request.Context(), issuerFromRequest(c), principal, domainprovider.AuthorizeInput{
		ResponseType:        c.Query("response_type"),
		ClientID:            c.Query("client_id"),
		RedirectURI:         c.Query("redirect_uri"),
		Scope:               c.Query("scope"),
		State:               c.Query("state"),
		Nonce:               c.Query("nonce"),
		CodeChallenge:       c.Query("code_challenge"),
		CodeChallengeMethod: c.Query("code_challenge_method"),
	})
	if err != nil {
		var redirectErr *domainprovider.AuthorizeRedirectError
		if errors.As(err, &redirectErr) {
			writeOIDCAuthorizeErrorRedirect(c, redirectErr)
			return
		}
		writeOIDCError(c, apierrors.StatusCode(err), "invalid_request", err.Error())
		return
	}
	target, err := url.Parse(result.RedirectURI)
	if err != nil {
		writeOIDCError(c, http.StatusBadRequest, "invalid_request", "redirect_uri is invalid")
		return
	}
	values := target.Query()
	values.Set("code", result.Code)
	if result.State != "" {
		values.Set("state", result.State)
	}
	target.RawQuery = values.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func (h *oidcHandler) OIDCToken(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	clientAuth := oidcClientAuthInputFromRequest(c)
	result, err := h.service.Token(c.Request.Context(), issuerFromRequest(c), domainprovider.TokenInput{
		GrantType:     c.PostForm("grant_type"),
		Code:          c.PostForm("code"),
		RedirectURI:   c.PostForm("redirect_uri"),
		ClientID:      clientAuth.ClientID,
		ClientSecret:  clientAuth.ClientSecret,
		CodeVerifier:  c.PostForm("code_verifier"),
		Authenticated: strings.TrimSpace(clientAuth.ClientSecret) != "",
	})
	if err != nil {
		status := apierrors.StatusCode(err)
		if status == http.StatusOK {
			status = http.StatusBadRequest
		}
		writeOIDCError(c, status, oauthErrorCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *oidcHandler) OIDCUserInfo(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	item, err := h.service.UserInfo(c.Request.Context(), issuerFromRequest(c), c.GetHeader("Authorization"))
	if err != nil {
		writeOIDCError(c, apierrors.StatusCode(err), "invalid_token", err.Error())
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *oidcHandler) OIDCIntrospect(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	item, err := h.service.Introspect(c.Request.Context(), issuerFromRequest(c), c.PostForm("token"), oidcClientAuthInputFromRequest(c))
	if err != nil {
		writeOIDCError(c, apierrors.StatusCode(err), oauthClientAuthErrorCode(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *oidcHandler) OIDCRevoke(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	if err := h.service.Revoke(c.Request.Context(), issuerFromRequest(c), c.PostForm("token"), oidcClientAuthInputFromRequest(c)); err != nil {
		writeOIDCError(c, apierrors.StatusCode(err), oauthClientAuthErrorCode(err), err.Error())
		return
	}
	c.Status(http.StatusOK)
}

func (h *oidcHandler) OIDCJWKS(c *gin.Context) {
	if h.service == nil {
		writeOIDCError(c, http.StatusServiceUnavailable, "server_error", "oidc provider service is not configured")
		return
	}
	items, err := h.service.JWKS(c.Request.Context())
	if err != nil {
		writeOIDCError(c, apierrors.StatusCode(err), "server_error", err.Error())
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *proxyHandler) ProxyAuth(c *gin.Context) {
	if h.service == nil {
		writeProxyError(c, http.StatusServiceUnavailable, "proxy provider service is not configured")
		return
	}
	result, err := h.service.ProxyAuth(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), proxyAuthInputFromRequest(c))
	if err != nil {
		writeError(c, err)
		return
	}
	writeProxyAuthResult(c, result)
}

func (h *proxyHandler) ProxyStart(c *gin.Context) {
	if h.service == nil {
		writeProxyError(c, http.StatusServiceUnavailable, "proxy provider service is not configured")
		return
	}
	target := proxyReturnTo(c)
	result, err := h.service.ProxyAuth(c.Request.Context(), domainidentity.Principal{}, proxyAuthInputForReturnTo(c, target))
	if err != nil {
		writeError(c, err)
		return
	}
	if result.Decision == domainprovider.ProxyDecisionAllow {
		c.Redirect(http.StatusFound, firstNonEmpty(result.OriginalURL, target, "/portal"))
		return
	}
	if result.Decision == domainprovider.ProxyDecisionDeny {
		writeProxyAuthResult(c, result)
		return
	}
	callback := "/api/v1/provider/proxy/callback?return_to=" + url.QueryEscape(target)
	if providerID := firstNonEmpty(c.Query("provider_id"), c.Query("providerID")); providerID != "" {
		callback += "&provider_id=" + url.QueryEscape(providerID)
	}
	c.Redirect(http.StatusFound, "/login?return_to="+url.QueryEscape(callback))
}

func (h *proxyHandler) ProxyCallback(c *gin.Context) {
	if h.service == nil {
		writeProxyError(c, http.StatusServiceUnavailable, "proxy provider service is not configured")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if strings.TrimSpace(principal.UserID) == "" {
		c.Redirect(http.StatusFound, "/login?return_to="+url.QueryEscape(c.Request.URL.RequestURI()))
		return
	}
	target := proxyReturnTo(c)
	result, err := h.service.ProxyAuth(c.Request.Context(), principal, proxyAuthInputForReturnTo(c, target))
	if err != nil {
		writeError(c, err)
		return
	}
	if result.Decision != domainprovider.ProxyDecisionAllow {
		writeProxyAuthResult(c, result)
		return
	}
	session, err := h.service.IssueProxySession(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	setProxySessionCookie(c, session, result.CookieDomain)
	c.Redirect(http.StatusFound, firstNonEmpty(result.OriginalURL, target, "/portal"))
}

func (h *proxyHandler) ProxyLogout(c *gin.Context) {
	cookieDomain := ""
	if h.service != nil {
		if domain, err := h.service.ProxyCookieDomain(c.Request.Context(), proxyAuthInputFromRequest(c)); err == nil {
			cookieDomain = domain
		}
	}
	clearProxySessionCookie(c, cookieDomain)
	c.String(http.StatusOK, "OK")
}

func proxyAuthInputFromRequest(c *gin.Context) domainprovider.ProxyAuthInput {
	return domainprovider.ProxyAuthInput{
		ProviderID:     firstNonEmpty(c.Query("provider_id"), c.Query("providerID")),
		OriginalURL:    firstNonEmpty(c.GetHeader("X-Original-URL"), c.GetHeader("X-Original-Uri"), c.Query("return_to")),
		ForwardedHost:  c.GetHeader("X-Forwarded-Host"),
		ForwardedProto: c.GetHeader("X-Forwarded-Proto"),
		ForwardedURI:   c.GetHeader("X-Forwarded-Uri"),
		RequestHost:    c.Request.Host,
		RequestPath:    c.Request.URL.RequestURI(),
		Method:         c.Request.Method,
		Redirect:       proxyRedirectRequested(c),
		SessionToken:   proxySessionTokenFromRequest(c),
	}
}

func proxyAuthInputForReturnTo(c *gin.Context, target string) domainprovider.ProxyAuthInput {
	return domainprovider.ProxyAuthInput{
		ProviderID:     firstNonEmpty(c.Query("provider_id"), c.Query("providerID")),
		OriginalURL:    strings.TrimSpace(target),
		ForwardedHost:  c.GetHeader("X-Forwarded-Host"),
		ForwardedProto: c.GetHeader("X-Forwarded-Proto"),
		ForwardedURI:   c.GetHeader("X-Forwarded-Uri"),
		RequestHost:    c.Request.Host,
		RequestPath:    c.Request.URL.RequestURI(),
		Method:         c.Request.Method,
		Redirect:       proxyRedirectRequested(c),
		SessionToken:   proxySessionTokenFromRequest(c),
	}
}

func fillOutpostCheckInputFromHeaders(c *gin.Context, input *domainprovider.OutpostCheckInput) {
	if input == nil {
		return
	}
	input.ProviderID = firstNonEmpty(input.ProviderID, c.Query("provider_id"), c.Query("providerID"))
	input.OriginalURL = firstNonEmpty(input.OriginalURL, c.GetHeader("X-Original-URL"), c.GetHeader("X-Original-Uri"), c.Query("return_to"))
	input.ForwardedHost = firstNonEmpty(input.ForwardedHost, c.GetHeader("X-Forwarded-Host"))
	input.ForwardedProto = firstNonEmpty(input.ForwardedProto, c.GetHeader("X-Forwarded-Proto"))
	input.ForwardedURI = firstNonEmpty(input.ForwardedURI, c.GetHeader("X-Forwarded-Uri"))
	input.RequestHost = firstNonEmpty(input.RequestHost, c.Request.Host)
	input.RequestPath = firstNonEmpty(input.RequestPath, c.Request.URL.RequestURI())
	input.Method = firstNonEmpty(input.Method, c.Request.Method)
	input.SessionToken = firstNonEmpty(input.SessionToken, proxySessionTokenFromRequest(c))
}

func proxySessionTokenFromRequest(c *gin.Context) string {
	if value, err := c.Cookie(proxySessionCookieName); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(c.GetHeader("X-Soha-Proxy-Session"))
}

func outpostTokenFromRequest(c *gin.Context, bodyValue string) string {
	if token := bearerTokenValue(c.GetHeader("Authorization")); token != "" {
		return token
	}
	return strings.TrimSpace(bodyValue)
}

func bearerTokenValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func oidcClientAuthInputFromRequest(c *gin.Context) domainprovider.ClientAuthInput {
	basicClientID, basicSecret, basicOK := parseBasicAuth(c.GetHeader("Authorization"))
	if basicOK {
		return domainprovider.ClientAuthInput{
			ClientID:     basicClientID,
			ClientSecret: basicSecret,
		}
	}
	return domainprovider.ClientAuthInput{
		ClientID:     c.PostForm("client_id"),
		ClientSecret: c.PostForm("client_secret"),
	}
}

func bindOptionalJSON(c *gin.Context, out any) error {
	if c.Request == nil || c.Request.Body == nil || c.Request.ContentLength == 0 {
		return nil
	}
	return c.ShouldBindJSON(out)
}

func setProxySessionCookie(c *gin.Context, session domainprovider.ProxySession, cookieDomain string) {
	token := strings.TrimSpace(session.Token)
	if token == "" {
		return
	}
	maxAge := int(time.Until(session.ExpiresAt) / time.Second)
	if maxAge <= 0 {
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(proxySessionCookieName, token, maxAge, "/", strings.TrimSpace(cookieDomain), requestIsHTTPS(c), true)
}

func clearProxySessionCookie(c *gin.Context, cookieDomain string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(proxySessionCookieName, "", -1, "/", strings.TrimSpace(cookieDomain), requestIsHTTPS(c), true)
}

func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}

func writeProxyAuthResult(c *gin.Context, result domainprovider.ProxyAuthResult) {
	switch result.Decision {
	case domainprovider.ProxyDecisionAllow:
		for key, value := range result.Headers {
			if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
				c.Header(key, value)
			}
		}
		c.Header("X-Soha-Proxy-Decision", domainprovider.ProxyDecisionAllow)
		if result.Skipped {
			c.Header("X-Soha-Proxy-Skipped", "true")
		}
		c.String(http.StatusOK, "OK")
	case domainprovider.ProxyDecisionLogin:
		loginURL := firstNonEmpty(result.LoginURL, "/login")
		c.Header("Location", loginURL)
		c.Header("X-Soha-Login-URL", loginURL)
		c.Header("X-Soha-Proxy-Decision", domainprovider.ProxyDecisionLogin)
		if proxyRedirectRequested(c) {
			c.Redirect(http.StatusFound, loginURL)
			return
		}
		c.String(http.StatusUnauthorized, result.Reason)
	case domainprovider.ProxyDecisionDeny:
		c.Header("X-Soha-Proxy-Decision", domainprovider.ProxyDecisionDeny)
		c.String(http.StatusForbidden, firstNonEmpty(result.Reason, "access denied"))
	default:
		writeProxyError(c, http.StatusInternalServerError, "unsupported proxy decision")
	}
}

func writeProxyError(c *gin.Context, status int, message string) {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	c.Header("X-Soha-Proxy-Decision", "error")
	c.String(status, message)
}

func proxyReturnTo(c *gin.Context) string {
	target := firstNonEmpty(
		c.Query("return_to"),
		c.Query("returnTo"),
		c.Query("rd"),
		c.GetHeader("X-Original-URL"),
		c.GetHeader("X-Original-Uri"),
	)
	if target == "" {
		proto := firstNonEmpty(c.GetHeader("X-Forwarded-Proto"), "https")
		host := firstNonEmpty(c.GetHeader("X-Forwarded-Host"), c.Request.Host)
		uri := firstNonEmpty(c.GetHeader("X-Forwarded-Uri"), "/")
		if host != "" {
			target = proto + "://" + host + uri
		}
	}
	if target == "" || strings.HasPrefix(target, "//") {
		return "/portal"
	}
	if _, err := url.Parse(target); err != nil {
		return "/portal"
	}
	return target
}

func proxyRedirectRequested(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query("redirect"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func issuerFromRequest(c *gin.Context) string {
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func parseBasicAuth(header string) (string, string, bool) {
	header = strings.TrimSpace(header)
	if len(header) < 6 || !strings.EqualFold(header[:6], "Basic ") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[6:]))
	if err != nil {
		return "", "", false
	}
	user, password, ok := strings.Cut(string(raw), ":")
	if !ok {
		return "", "", false
	}
	return user, password, true
}

func oauthErrorCode(err error) string {
	switch {
	case errors.Is(err, apperrors.ErrUnauthorized):
		return "invalid_grant"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		return "invalid_request"
	case errors.Is(err, apperrors.ErrAccessDenied):
		return "access_denied"
	default:
		return "server_error"
	}
}

func oauthClientAuthErrorCode(err error) string {
	if errors.Is(err, apperrors.ErrUnauthorized) {
		return "invalid_client"
	}
	return oauthErrorCode(err)
}

func writeOIDCAuthorizeErrorRedirect(c *gin.Context, redirectErr *domainprovider.AuthorizeRedirectError) {
	target, err := url.Parse(redirectErr.RedirectURI)
	if err != nil || !target.IsAbs() || target.Fragment != "" {
		writeOIDCError(c, http.StatusBadRequest, "invalid_request", "redirect_uri is invalid")
		return
	}
	values := target.Query()
	code := strings.TrimSpace(redirectErr.Code)
	if code == "" {
		code = oauthAuthorizeErrorCode(redirectErr)
	}
	description := strings.TrimSpace(redirectErr.Description)
	if description == "" {
		description = redirectErr.Error()
	}
	values.Set("error", code)
	if description != "" {
		values.Set("error_description", description)
	}
	if state := strings.TrimSpace(redirectErr.State); state != "" {
		values.Set("state", state)
	}
	target.RawQuery = values.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func oauthAuthorizeErrorCode(err error) string {
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied), errors.Is(err, apperrors.ErrUnauthorized):
		return "access_denied"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		return "invalid_request"
	default:
		return "server_error"
	}
}

func writeOIDCError(c *gin.Context, status int, code, description string) {
	if status <= 0 || status == http.StatusOK {
		status = http.StatusBadRequest
	}
	c.JSON(status, gin.H{
		"error":             code,
		"error_description": description,
	})
}

func parseLimit(value string, fallback int) int {
	limit, err := strconv.Atoi(value)
	if value == "" || err != nil || limit <= 0 {
		return fallback
	}
	return limit
}

func parseOffset(value string) int {
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func writeError(c *gin.Context, err error) {
	if err == nil {
		err = errors.New("handler returned a nil error")
	}
	_ = c.Error(err)
	apiresponse.Error(c, apierrors.StatusCode(err), apierrors.Code(err), apierrors.Message(err))
}
