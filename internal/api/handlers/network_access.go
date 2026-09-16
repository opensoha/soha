package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NetworkAccessService interface {
	ListDevices(context.Context, domainidentity.Principal, domainnetworkaccess.DeviceFilter) ([]domainnetworkaccess.Device, error)
	GetDevice(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Device, error)
	RegisterDevice(context.Context, domainidentity.Principal, string, domainnetworkaccess.DeviceRegistrationInput) (domainnetworkaccess.Device, error)
	UpdateDevice(context.Context, domainidentity.Principal, string, domainnetworkaccess.DeviceInput) (domainnetworkaccess.Device, error)
	ListSites(context.Context, domainidentity.Principal, domainnetworkaccess.SiteFilter) ([]domainnetworkaccess.Site, error)
	GetSite(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Site, error)
	CreateSite(context.Context, domainidentity.Principal, domainnetworkaccess.SiteInput) (domainnetworkaccess.Site, error)
	UpdateSite(context.Context, domainidentity.Principal, string, domainnetworkaccess.SiteInput) (domainnetworkaccess.Site, error)
	DeleteSite(context.Context, domainidentity.Principal, string) error
	ListSpaces(context.Context, domainidentity.Principal, domainnetworkaccess.SpaceFilter) ([]domainnetworkaccess.Space, error)
	GetSpace(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Space, error)
	CreateSpace(context.Context, domainidentity.Principal, domainnetworkaccess.SpaceInput) (domainnetworkaccess.Space, error)
	UpdateSpace(context.Context, domainidentity.Principal, string, domainnetworkaccess.SpaceInput) (domainnetworkaccess.Space, error)
	DeleteSpace(context.Context, domainidentity.Principal, string) error
	ListResources(context.Context, domainidentity.Principal, domainnetworkaccess.ResourceFilter) ([]domainnetworkaccess.Resource, error)
	GetResource(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Resource, error)
	CreateResource(context.Context, domainidentity.Principal, domainnetworkaccess.ResourceInput) (domainnetworkaccess.Resource, error)
	UpdateResource(context.Context, domainidentity.Principal, string, domainnetworkaccess.ResourceInput) (domainnetworkaccess.Resource, error)
	DeleteResource(context.Context, domainidentity.Principal, string) error
	ListGateways(context.Context, domainidentity.Principal, domainnetworkaccess.GatewayFilter) ([]domainnetworkaccess.Gateway, error)
	GetGateway(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Gateway, error)
	CreateGateway(context.Context, domainidentity.Principal, domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error)
	UpdateGateway(context.Context, domainidentity.Principal, string, domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error)
	ListMihomoProfiles(context.Context, domainidentity.Principal, domainnetworkaccess.MihomoProfileFilter) ([]domainnetworkaccess.MihomoProfile, error)
	GetMihomoProfile(context.Context, domainidentity.Principal, string) (domainnetworkaccess.MihomoProfile, error)
	CreateMihomoProfile(context.Context, domainidentity.Principal, domainnetworkaccess.MihomoProfileInput) (domainnetworkaccess.MihomoProfile, error)
	UpdateMihomoProfile(context.Context, domainidentity.Principal, string, domainnetworkaccess.MihomoProfileInput) (domainnetworkaccess.MihomoProfile, error)
	DeleteMihomoProfile(context.Context, domainidentity.Principal, string) error
	ListConnectionOptions(context.Context, domainidentity.Principal, string) ([]domainnetworkaccess.ConnectionOption, error)
	ListNASBindings(context.Context, domainidentity.Principal, domainnetworkaccess.NASBindingFilter) ([]domainnetworkaccess.NASBinding, error)
	GetNASBinding(context.Context, domainidentity.Principal, string) (domainnetworkaccess.NASBinding, error)
	CreateNASBinding(context.Context, domainidentity.Principal, domainnetworkaccess.NASBindingInput) (domainnetworkaccess.NASBinding, error)
	UpdateNASBinding(context.Context, domainidentity.Principal, string, domainnetworkaccess.NASBindingInput) (domainnetworkaccess.NASBinding, error)
	DeleteNASBinding(context.Context, domainidentity.Principal, string) error
	ListSiteProfileBindings(context.Context, domainidentity.Principal, domainnetworkaccess.SiteProfileBindingFilter) ([]domainnetworkaccess.SiteProfileBinding, error)
	GetSiteProfileBinding(context.Context, domainidentity.Principal, string) (domainnetworkaccess.SiteProfileBinding, error)
	CreateSiteProfileBinding(context.Context, domainidentity.Principal, domainnetworkaccess.SiteProfileBindingInput) (domainnetworkaccess.SiteProfileBinding, error)
	UpdateSiteProfileBinding(context.Context, domainidentity.Principal, string, domainnetworkaccess.SiteProfileBindingInput) (domainnetworkaccess.SiteProfileBinding, error)
	DeleteSiteProfileBinding(context.Context, domainidentity.Principal, string) error
	ListSessions(context.Context, domainidentity.Principal, domainnetworkaccess.SessionFilter) ([]domainnetworkaccess.Session, error)
	GetSession(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Session, error)
	PlanSessionAction(context.Context, domainidentity.Principal, string, domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionActionPlan, error)
	ExecuteSessionAction(context.Context, domainidentity.Principal, string, domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionCommand, error)
	ListPolicies(context.Context, domainidentity.Principal, domainnetworkaccess.PolicyFilter) ([]domainnetworkaccess.Policy, error)
	GetPolicy(context.Context, domainidentity.Principal, string) (domainnetworkaccess.Policy, error)
	CreatePolicy(context.Context, domainidentity.Principal, domainnetworkaccess.PolicyInput) (domainnetworkaccess.Policy, error)
	UpdatePolicy(context.Context, domainidentity.Principal, string, domainnetworkaccess.PolicyInput) (domainnetworkaccess.Policy, error)
	DeletePolicy(context.Context, domainidentity.Principal, string) error
	CompilePolicySnapshot(context.Context, domainidentity.Principal) (domainnetworkaccess.PolicySnapshot, error)
	GetPolicySnapshot(context.Context, domainidentity.Principal) (domainnetworkaccess.PolicySnapshot, error)
	AnalyzeConflicts(context.Context, domainidentity.Principal, []domainnetworkaccess.ConflictRange) (domainnetworkaccess.ConflictAnalysis, error)
	PreviewPolicy(context.Context, domainidentity.Principal, appnetworkaccess.PreviewInput) (domainnetworkaccess.PolicyPreview, error)
}

type NetworkEnrollmentService interface {
	Create(context.Context, domainidentity.Principal, appnetworkaccess.EnrollmentInput) (domainnetworkruntime.EnrollmentSecret, error)
	List(context.Context, domainidentity.Principal, int) ([]domainnetworkruntime.EnrollmentChallenge, error)
	Get(context.Context, domainidentity.Principal, string) (domainnetworkruntime.EnrollmentChallenge, error)
	Revoke(context.Context, domainidentity.Principal, string) (domainnetworkruntime.EnrollmentChallenge, error)
}

type NetworkAccessGrantService interface {
	Create(context.Context, domainidentity.Principal, string, appnetworkaccess.AccessGrantInput) (domainnetworkruntime.AccessGrantSecret, error)
	List(context.Context, domainidentity.Principal, domainnetworkruntime.AccessGrantFilter) ([]domainnetworkruntime.AccessGrant, error)
	Get(context.Context, domainidentity.Principal, string) (domainnetworkruntime.AccessGrant, error)
	Revoke(context.Context, domainidentity.Principal, string) (domainnetworkruntime.AccessGrant, error)
}

type NetworkTelemetryService interface {
	Summary(context.Context, domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error)
}

type NetworkAccessHandler struct {
	service     NetworkAccessService
	enrollments NetworkEnrollmentService
	grants      NetworkAccessGrantService
	telemetry   NetworkTelemetryService
}

func NewNetworkAccessHandler(service NetworkAccessService, enrollments NetworkEnrollmentService, grants NetworkAccessGrantService, telemetry NetworkTelemetryService) *NetworkAccessHandler {
	return &NetworkAccessHandler{service: service, enrollments: enrollments, grants: grants, telemetry: telemetry}
}

func (h *NetworkAccessHandler) ListDevices(c *gin.Context) {
	var params sohaapi.ListEndpointDevicesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListDevices(c.Request.Context(), principal(c), domainnetworkaccess.DeviceFilter{Search: params.Search, OwnerUserID: params.OwnerUserID, SiteID: params.SiteID, Status: string(params.Status), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetDevice(c *gin.Context) {
	item, err := h.service.GetDevice(c.Request.Context(), principal(c), c.Param("deviceID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) RegisterDevice(c *gin.Context) {
	var input sohaapi.EndpointDeviceRegistrationInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.RegisterDevice(c.Request.Context(), principal(c), c.Param("deviceID"), domainnetworkaccess.DeviceRegistrationInput{Name: input.Name, Hostname: input.Hostname, Platform: input.Platform, DeviceType: string(input.DeviceType), ReportedFacts: endpointDeviceReportedFacts(input.ReportedFacts)})
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) UpdateDevice(c *gin.Context) {
	var input sohaapi.EndpointDeviceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateDevice(c.Request.Context(), principal(c), c.Param("deviceID"), domainnetworkaccess.DeviceInput{Name: input.Name, SiteID: input.SiteID, Status: string(input.Status), PostureStatus: string(input.PostureStatus), DeviceType: string(input.DeviceType), OwnershipType: string(input.OwnershipType)})
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) ListConnectionOptions(c *gin.Context) {
	var params sohaapi.ListCurrentNetworkConnectionOptionsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListConnectionOptions(c.Request.Context(), principal(c), params.DeviceID)
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.NetworkConnectionOption, len(items))
	for index, item := range items {
		result[index] = sohaapi.NetworkConnectionOption{
			SiteID: item.SiteID, SiteName: item.SiteName, AccessMedium: sohaapi.NetworkAccessMedium(item.AccessMedium),
			Ssid: item.SSID, Authentication: sohaapi.NetworkConnectionAuthentication(item.Authentication),
			AccessProfile: sohaapi.NetworkAccessProfile(item.AccessProfile), PolicyVersion: item.PolicyVersion,
		}
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *NetworkAccessHandler) ListSites(c *gin.Context) {
	var params sohaapi.ListNetworkSitesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListSites(c.Request.Context(), principal(c), domainnetworkaccess.SiteFilter{Search: params.Search, Status: string(params.Status), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetSite(c *gin.Context) {
	item, err := h.service.GetSite(c.Request.Context(), principal(c), c.Param("siteID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateSite(c *gin.Context) {
	var input sohaapi.NetworkSiteInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateSite(c.Request.Context(), principal(c), domainnetworkaccess.SiteInput{Name: input.Name, Description: input.Description, Location: input.Location, Status: string(input.Status)})
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateSite(c *gin.Context) {
	var input sohaapi.NetworkSiteInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateSite(c.Request.Context(), principal(c), c.Param("siteID"), domainnetworkaccess.SiteInput{Name: input.Name, Description: input.Description, Location: input.Location, Status: string(input.Status)})
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeleteSite(c *gin.Context) {
	respondDelete(c, h.service.DeleteSite(c.Request.Context(), principal(c), c.Param("siteID")))
}

func (h *NetworkAccessHandler) ListSpaces(c *gin.Context) {
	var params sohaapi.ListNetworkSpacesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListSpaces(c.Request.Context(), principal(c), domainnetworkaccess.SpaceFilter{Search: params.Search, SiteID: params.SiteID, Status: string(params.Status), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetSpace(c *gin.Context) {
	item, err := h.service.GetSpace(c.Request.Context(), principal(c), c.Param("spaceID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateSpace(c *gin.Context) {
	var input sohaapi.NetworkSpaceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateSpace(c.Request.Context(), principal(c), domainnetworkaccess.SpaceInput{SiteID: input.SiteID, Name: input.Name, CIDRs: input.Cidrs, Status: string(input.Status)})
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateSpace(c *gin.Context) {
	var input sohaapi.NetworkSpaceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateSpace(c.Request.Context(), principal(c), c.Param("spaceID"), domainnetworkaccess.SpaceInput{SiteID: input.SiteID, Name: input.Name, CIDRs: input.Cidrs, Status: string(input.Status)})
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeleteSpace(c *gin.Context) {
	respondDelete(c, h.service.DeleteSpace(c.Request.Context(), principal(c), c.Param("spaceID")))
}

func (h *NetworkAccessHandler) ListResources(c *gin.Context) {
	var params sohaapi.ListNetworkResourcesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	var protected *bool
	if raw, exists := c.GetQuery("protected"); exists {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid network access query")
			return
		}
		protected = &value
	}
	items, err := h.service.ListResources(c.Request.Context(), principal(c), domainnetworkaccess.ResourceFilter{Search: params.Search, SpaceID: params.SpaceID, Kind: string(params.Kind), Protected: protected, Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetResource(c *gin.Context) {
	item, err := h.service.GetResource(c.Request.Context(), principal(c), c.Param("resourceID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateResource(c *gin.Context) {
	var input sohaapi.NetworkResourceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateResource(c.Request.Context(), principal(c), resourceInput(input))
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateResource(c *gin.Context) {
	var input sohaapi.NetworkResourceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateResource(c.Request.Context(), principal(c), c.Param("resourceID"), resourceInput(input))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeleteResource(c *gin.Context) {
	respondDelete(c, h.service.DeleteResource(c.Request.Context(), principal(c), c.Param("resourceID")))
}

func (h *NetworkAccessHandler) ListGateways(c *gin.Context) {
	var params sohaapi.ListNetworkGatewaysParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListGateways(c.Request.Context(), principal(c), domainnetworkaccess.GatewayFilter{Search: params.Search, SiteID: params.SiteID, Status: string(params.Status), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetGateway(c *gin.Context) {
	item, err := h.service.GetGateway(c.Request.Context(), principal(c), c.Param("gatewayID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateGateway(c *gin.Context) {
	var input sohaapi.NetworkGatewayInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateGateway(c.Request.Context(), principal(c), gatewayInput(input))
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateGateway(c *gin.Context) {
	var input sohaapi.NetworkGatewayInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateGateway(c.Request.Context(), principal(c), c.Param("gatewayID"), gatewayInput(input))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) ListMihomoProfiles(c *gin.Context) {
	var params sohaapi.ListNetworkMihomoProfilesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListMihomoProfiles(c.Request.Context(), principal(c), domainnetworkaccess.MihomoProfileFilter{
		Search: params.Search, DeviceID: params.DeviceID, Mode: string(params.Mode), Status: string(params.Status), Limit: params.Limit,
	})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetMihomoProfile(c *gin.Context) {
	item, err := h.service.GetMihomoProfile(c.Request.Context(), principal(c), c.Param("profileID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateMihomoProfile(c *gin.Context) {
	var input sohaapi.NetworkMihomoProfileInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateMihomoProfile(c.Request.Context(), principal(c), mihomoProfileInput(input))
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateMihomoProfile(c *gin.Context) {
	var input sohaapi.NetworkMihomoProfileInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateMihomoProfile(c.Request.Context(), principal(c), c.Param("profileID"), mihomoProfileInput(input))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeleteMihomoProfile(c *gin.Context) {
	respondDelete(c, h.service.DeleteMihomoProfile(c.Request.Context(), principal(c), c.Param("profileID")))
}

func (h *NetworkAccessHandler) ListNASBindings(c *gin.Context) {
	var params sohaapi.ListNetworkNASBindingsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListNASBindings(c.Request.Context(), principal(c), domainnetworkaccess.NASBindingFilter{SiteID: params.SiteID, RuntimeID: params.RuntimeID, Status: string(params.Status), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetNASBinding(c *gin.Context) {
	item, err := h.service.GetNASBinding(c.Request.Context(), principal(c), c.Param("bindingID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateNASBinding(c *gin.Context) {
	var input sohaapi.NetworkNASBindingInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateNASBinding(c.Request.Context(), principal(c), nasBindingInput(input))
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateNASBinding(c *gin.Context) {
	var input sohaapi.NetworkNASBindingInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateNASBinding(c.Request.Context(), principal(c), c.Param("bindingID"), nasBindingInput(input))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeleteNASBinding(c *gin.Context) {
	respondDelete(c, h.service.DeleteNASBinding(c.Request.Context(), principal(c), c.Param("bindingID")))
}

func (h *NetworkAccessHandler) ListSiteProfileBindings(c *gin.Context) {
	var params sohaapi.ListNetworkSiteProfileBindingsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListSiteProfileBindings(c.Request.Context(), principal(c), domainnetworkaccess.SiteProfileBindingFilter{SiteID: params.SiteID, AccessProfile: string(params.AccessProfile), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetSiteProfileBinding(c *gin.Context) {
	item, err := h.service.GetSiteProfileBinding(c.Request.Context(), principal(c), c.Param("bindingID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreateSiteProfileBinding(c *gin.Context) {
	var input sohaapi.NetworkSiteProfileBindingInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreateSiteProfileBinding(c.Request.Context(), principal(c), siteProfileBindingInput(input))
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdateSiteProfileBinding(c *gin.Context) {
	var input sohaapi.NetworkSiteProfileBindingInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateSiteProfileBinding(c.Request.Context(), principal(c), c.Param("bindingID"), siteProfileBindingInput(input))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeleteSiteProfileBinding(c *gin.Context) {
	respondDelete(c, h.service.DeleteSiteProfileBinding(c.Request.Context(), principal(c), c.Param("bindingID")))
}

func (h *NetworkAccessHandler) ListSessions(c *gin.Context) {
	var params sohaapi.ListNetworkSessionsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.service.ListSessions(c.Request.Context(), principal(c), domainnetworkaccess.SessionFilter{
		SiteID: params.SiteID, RuntimeID: params.RuntimeID, SubjectID: params.SubjectID,
		DeviceID: params.DeviceID, Status: string(params.Status), Limit: params.Limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.NetworkSession, len(items))
	for index, item := range items {
		result[index] = sessionDTO(item)
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *NetworkAccessHandler) GetTelemetrySummary(c *gin.Context) {
	var params sohaapi.GetNetworkTelemetrySummaryParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	if h.telemetry == nil {
		writeError(c, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "network_ingest_unavailable", "Network telemetry is temporarily unavailable.", "网络遥测暂时不可用。"))
		return
	}
	item, err := h.telemetry.Summary(c.Request.Context(), domainnetworkingest.SummaryFilter{From: params.From, To: params.To, ProducerID: params.ProducerID, Limit: params.Limit})
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) GetSession(c *gin.Context) {
	item, err := h.service.GetSession(c.Request.Context(), principal(c), c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, sessionDTO(item))
}

func (h *NetworkAccessHandler) PlanSessionAction(c *gin.Context) {
	var input sohaapi.NetworkSessionActionInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.PlanSessionAction(c.Request.Context(), principal(c), c.Param("sessionID"), domainnetworkaccess.SessionActionInput{
		Action: string(input.Action), TargetAccessProfile: string(input.TargetAccessProfile), ReasonCode: input.ReasonCode,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, sessionActionPlanDTO(item))
}

func (h *NetworkAccessHandler) ExecuteSessionAction(c *gin.Context) {
	var input sohaapi.NetworkSessionActionExecuteInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.ExecuteSessionAction(c.Request.Context(), principal(c), c.Param("sessionID"), domainnetworkaccess.SessionActionInput{
		Action: string(input.Action), TargetAccessProfile: string(input.TargetAccessProfile), ReasonCode: input.ReasonCode, PlanHash: input.PlanHash,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, sessionCommandDTO(item))
}

func (h *NetworkAccessHandler) ListAccessGrants(c *gin.Context) {
	var params sohaapi.ListNetworkAccessGrantsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.grants.List(c.Request.Context(), principal(c), domainnetworkruntime.AccessGrantFilter{
		SubjectID: params.SubjectID, DeviceID: params.DeviceID, Status: string(params.Status), Limit: params.Limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.NetworkAccessGrant, len(items))
	for index, item := range items {
		result[index] = accessGrantDTO(item)
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *NetworkAccessHandler) CreateAccessGrant(c *gin.Context) {
	var input sohaapi.NetworkAccessGrantInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	secret, err := h.grants.Create(c.Request.Context(), principal(c), apiMiddleware.AccessContextFromContext(c).SessionID, appnetworkaccess.AccessGrantInput{
		DeviceID: input.DeviceID, SiteID: input.SiteID, NetworkSpaceID: input.NetworkSpaceID,
		Mode: string(input.Mode), ResourceIDs: input.ResourceIDs, TTL: time.Duration(input.TTLSeconds) * time.Second,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusCreated, sohaapi.NetworkAccessGrantSecret{Grant: accessGrantDTO(secret.Grant), Token: secret.Token})
}

func (h *NetworkAccessHandler) GetAccessGrant(c *gin.Context) {
	item, err := h.grants.Get(c.Request.Context(), principal(c), c.Param("grantID"))
	respondItem(c, http.StatusOK, accessGrantDTO(item), err)
}

func (h *NetworkAccessHandler) RevokeAccessGrant(c *gin.Context) {
	item, err := h.grants.Revoke(c.Request.Context(), principal(c), c.Param("grantID"))
	respondItem(c, http.StatusOK, accessGrantDTO(item), err)
}

func (h *NetworkAccessHandler) ListEnrollments(c *gin.Context) {
	var params sohaapi.ListNetworkRuntimeEnrollmentsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.enrollments.List(c.Request.Context(), principal(c), params.Limit)
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.NetworkRuntimeEnrollment, len(items))
	for index, item := range items {
		result[index] = enrollmentDTO(item)
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *NetworkAccessHandler) CreateEnrollment(c *gin.Context) {
	var input sohaapi.NetworkRuntimeEnrollmentInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	secret, err := h.enrollments.Create(c.Request.Context(), principal(c), appnetworkaccess.EnrollmentInput{
		RuntimeID: input.RuntimeID, RuntimeKind: string(input.RuntimeKind), DeviceID: input.DeviceID,
		SubjectID: input.SubjectID, TTL: time.Duration(input.TTLSeconds) * time.Second,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusCreated, sohaapi.NetworkRuntimeEnrollmentSecret{Enrollment: enrollmentDTO(secret.EnrollmentChallenge), Token: secret.Token})
}

func (h *NetworkAccessHandler) GetEnrollment(c *gin.Context) {
	item, err := h.enrollments.Get(c.Request.Context(), principal(c), c.Param("enrollmentID"))
	respondItem(c, http.StatusOK, enrollmentDTO(item), err)
}

func (h *NetworkAccessHandler) RevokeEnrollment(c *gin.Context) {
	item, err := h.enrollments.Revoke(c.Request.Context(), principal(c), c.Param("enrollmentID"))
	respondItem(c, http.StatusOK, enrollmentDTO(item), err)
}

func (h *NetworkAccessHandler) ListPolicies(c *gin.Context) {
	var params sohaapi.ListNetworkAccessPoliciesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	var enabled *bool
	if raw, exists := c.GetQuery("enabled"); exists {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid network access query")
			return
		}
		enabled = &value
	}
	items, err := h.service.ListPolicies(c.Request.Context(), principal(c), domainnetworkaccess.PolicyFilter{Search: params.Search, Enabled: enabled, Effect: string(params.Effect), Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) GetPolicy(c *gin.Context) {
	item, err := h.service.GetPolicy(c.Request.Context(), principal(c), c.Param("policyID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) CreatePolicy(c *gin.Context) {
	var input sohaapi.NetworkAccessPolicyInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.CreatePolicy(c.Request.Context(), principal(c), policyInput(input))
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkAccessHandler) UpdatePolicy(c *gin.Context) {
	var input sohaapi.NetworkAccessPolicyInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdatePolicy(c.Request.Context(), principal(c), c.Param("policyID"), policyInput(input))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) DeletePolicy(c *gin.Context) {
	respondDelete(c, h.service.DeletePolicy(c.Request.Context(), principal(c), c.Param("policyID")))
}

func (h *NetworkAccessHandler) CompilePolicySnapshot(c *gin.Context) {
	item, err := h.service.CompilePolicySnapshot(c.Request.Context(), principal(c))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) GetPolicySnapshot(c *gin.Context) {
	item, err := h.service.GetPolicySnapshot(c.Request.Context(), principal(c))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkAccessHandler) AnalyzeConflicts(c *gin.Context) {
	var input sohaapi.NetworkConflictAnalysisRequest
	if !bindNetworkJSON(c, &input) {
		return
	}
	ranges := make([]domainnetworkaccess.ConflictRange, len(input.RuntimeRanges))
	for index, item := range input.RuntimeRanges {
		ranges[index] = domainnetworkaccess.ConflictRange{SourceType: string(item.SourceType), SourceID: item.SourceID, Name: item.Name, CIDR: item.Cidr}
	}
	result, err := h.service.AnalyzeConflicts(c.Request.Context(), principal(c), ranges)
	respondItem(c, http.StatusOK, result, err)
}

func (h *NetworkAccessHandler) PreviewPolicy(c *gin.Context) {
	var input sohaapi.NetworkPolicyPreviewRequest
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.PreviewPolicy(c.Request.Context(), principal(c), appnetworkaccess.PreviewInput{SubjectUserID: input.SubjectUserID, DeviceID: input.DeviceID, ResourceID: input.ResourceID, SiteID: input.SiteID, Mode: string(input.Mode)})
	respondItem(c, http.StatusOK, item, err)
}

func resourceInput(input sohaapi.NetworkResourceInput) domainnetworkaccess.ResourceInput {
	return domainnetworkaccess.ResourceInput{SpaceID: input.SpaceID, Name: input.Name, Kind: string(input.Kind), Target: input.Target, Protocol: string(input.Protocol), Ports: input.Ports, Protected: input.Protected, PathMode: string(input.PathMode)}
}

func endpointDeviceReportedFacts(input *sohaapi.EndpointDeviceReportedFacts) *domainnetworkaccess.DeviceReportedFacts {
	if input == nil {
		return nil
	}
	interfaces := make([]domainnetworkaccess.DeviceNetworkInterface, len(input.NetworkInterfaces))
	for index, item := range input.NetworkInterfaces {
		interfaces[index] = domainnetworkaccess.DeviceNetworkInterface{
			Name: item.Name, DisplayName: item.DisplayName, Kind: string(item.Kind), Status: string(item.Status), MACAddress: item.MacAddress,
			IPv4Addresses: item.Ipv4Addresses, IPv6Addresses: item.Ipv6Addresses, DNSServers: item.DNSServers,
		}
	}
	return &domainnetworkaccess.DeviceReportedFacts{
		OSName: input.OsName, OSVersion: input.OsVersion, OSBuild: input.OsBuild, Architecture: input.Architecture,
		Manufacturer: input.Manufacturer, Model: input.Model, SerialNumber: input.SerialNumber,
		AgentVersion: input.AgentVersion, CollectedAt: input.CollectedAt, NetworkInterfaces: interfaces,
	}
}

func gatewayInput(input sohaapi.NetworkGatewayInput) domainnetworkaccess.GatewayInput {
	advertisedCIDRs := make([]string, len(input.AdvertisedCidrs))
	for index := range input.AdvertisedCidrs {
		advertisedCIDRs[index] = string(input.AdvertisedCidrs[index])
	}
	return domainnetworkaccess.GatewayInput{
		Region: input.Region, ProviderCode: input.ProviderCode, ProviderName: input.ProviderName,
		SelectionPriority: input.SelectionPriority, AcceptNewConnections: input.AcceptNewConnections,
		MaxSessions: input.MaxSessions, ProbeURL: input.ProbeURL,
		RuntimeID: input.RuntimeID, SiteID: input.SiteID, Name: input.Name, AdministrativeStatus: string(input.AdministrativeStatus),
		PublicEndpointHost: input.PublicEndpointHost, PublicEndpointPort: input.PublicEndpointPort,
		OverlayCIDR: input.OverlayCidr, RoutingMode: string(input.RoutingMode), HubGatewayID: input.HubGatewayID,
		AdvertisedCIDRs: advertisedCIDRs, MTU: input.Mtu,
		PersistentKeepaliveSeconds: input.PersistentKeepaliveSeconds, DNSServers: input.DNSServers,
	}
}

func mihomoProfileInput(input sohaapi.NetworkMihomoProfileInput) domainnetworkaccess.MihomoProfileInput {
	var subscriptionURL *string
	if input.SubscriptionURL != "" {
		value := input.SubscriptionURL
		subscriptionURL = &value
	}
	var manualNode *domainnetworkaccess.MihomoManualNode
	if input.ManualNode != nil {
		manualNode = &domainnetworkaccess.MihomoManualNode{
			Protocol: string(input.ManualNode.Protocol), Server: input.ManualNode.Server, Port: input.ManualNode.Port,
			Username: input.ManualNode.Username, Password: input.ManualNode.Password,
		}
	}
	return domainnetworkaccess.MihomoProfileInput{
		DeviceID: input.DeviceID, Name: input.Name, Mode: string(input.Mode), SourceType: string(input.SourceType), Status: string(input.Status),
		SubscriptionURL: subscriptionURL, ManualNode: manualNode, MixedPort: input.MixedPort, ControllerPort: input.ControllerPort,
		DNSMode: string(input.DNSMode), FakeIPRange: input.FakeIPRange, SelectorGroup: input.SelectorGroup,
		SelectedProxy: input.SelectedProxy, BypassCIDRs: input.BypassCidrs, BypassHosts: input.BypassHosts,
		FailClosed: input.FailClosed,
	}
}

func nasBindingInput(input sohaapi.NetworkNASBindingInput) domainnetworkaccess.NASBindingInput {
	return domainnetworkaccess.NASBindingInput{
		NASID: input.NasID, RuntimeID: input.RuntimeID, SiteID: input.SiteID, Name: input.Name,
		AccessMedium: string(input.AccessMedium), DeviceType: string(input.DeviceType), SSID: input.Ssid, ManagementAddress: input.ManagementAddress,
		Status: string(input.Status), CoASupported: input.CoaSupported, DisconnectSupported: input.DisconnectSupported,
	}
}

func siteProfileBindingInput(input sohaapi.NetworkSiteProfileBindingInput) domainnetworkaccess.SiteProfileBindingInput {
	return domainnetworkaccess.SiteProfileBindingInput{SiteID: input.SiteID, AccessProfile: string(input.AccessProfile), VLANID: input.VlanID, FilterID: input.FilterID, SessionTimeoutSeconds: input.SessionTimeoutSeconds}
}

func enrollmentDTO(item domainnetworkruntime.EnrollmentChallenge) sohaapi.NetworkRuntimeEnrollment {
	return sohaapi.NetworkRuntimeEnrollment{
		ID: item.ID, ChallengeID: item.ChallengeID, RuntimeID: item.RuntimeID, RuntimeKind: sohaapi.NetworkRuntimeKind(item.RuntimeKind),
		DeviceID: item.DeviceID, SubjectID: item.SubjectID, Status: sohaapi.NetworkRuntimeEnrollmentStatus(item.Status),
		ExpiresAt: item.ExpiresAt, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt, ConsumedAt: item.ConsumedAt, RevokedAt: item.RevokedAt,
	}
}

func accessGrantDTO(item domainnetworkruntime.AccessGrant) sohaapi.NetworkAccessGrant {
	return sohaapi.NetworkAccessGrant{
		ID: item.ID, SubjectID: item.SubjectID, DeviceID: item.DeviceID, SiteID: item.SiteID,
		NetworkSpaceID: item.NetworkSpaceID, Mode: sohaapi.NetworkAccessGrantMode(item.Mode), ResourceIDs: item.ResourceIDs,
		PolicyVersion: item.PolicyVersion, Status: sohaapi.NetworkAccessGrantStatus(item.Status), SessionID: item.SessionID,
		ResourceLeaseIDs: item.ResourceLeaseIDs, ReasonCode: item.ReasonCode, ExpiresAt: item.ExpiresAt,
		ConsumedAt: item.ConsumedAt, RevokedAt: item.RevokedAt, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt,
	}
}

func sessionDTO(item domainnetworkaccess.Session) sohaapi.NetworkSession {
	return sohaapi.NetworkSession{
		ID: item.ID, SubjectID: item.SubjectID, DeviceID: item.DeviceID, SiteID: item.SiteID,
		GatewayID: item.GatewayID, NasID: item.NASID, Mode: sohaapi.NetworkAccessMode(item.Mode),
		Path: sohaapi.NetworkPolicyPath(item.Path), AccessProfile: sohaapi.NetworkAccessProfile(item.AccessProfile),
		Status: sohaapi.NetworkSessionStatus(item.Status), PolicyVersion: item.PolicyVersion,
		NetworkLeaseIDs: item.NetworkLeaseIDs, ResourceLeaseIDs: item.ResourceLeaseIDs,
		ReasonCode: item.ReasonCode, StartedAt: item.StartedAt, ExpiresAt: item.ExpiresAt,
	}
}

func sessionActionPlanDTO(item domainnetworkaccess.SessionActionPlan) sohaapi.NetworkSessionActionPlan {
	return sohaapi.NetworkSessionActionPlan{
		SessionID: item.SessionID, RuntimeID: item.RuntimeID, NasID: item.NASID,
		RequestedAction: sohaapi.NetworkSessionAction(item.RequestedAction), EffectiveAction: sohaapi.NetworkSessionAction(item.EffectiveAction),
		CurrentAccessProfile: sohaapi.NetworkAccessProfile(item.CurrentAccessProfile), TargetAccessProfile: sohaapi.NetworkAccessProfile(item.TargetAccessProfile),
		ReasonCode: item.ReasonCode, WillDisconnect: item.WillDisconnect, CommandExpiresAt: item.CommandExpiresAt, PlanHash: item.PlanHash,
	}
}

func sessionCommandDTO(item domainnetworkaccess.SessionCommand) sohaapi.NetworkSessionCommand {
	return sohaapi.NetworkSessionCommand{
		ID: item.ID, SessionID: item.SessionID, RuntimeID: item.RuntimeID, NasID: item.NASID,
		Action: sohaapi.NetworkSessionAction(item.Action), TargetAccessProfile: sohaapi.NetworkAccessProfile(item.TargetAccessProfile),
		PolicyVersion: item.PolicyVersion, Status: sohaapi.NetworkSessionCommandStatus(item.Status), ReasonCode: item.ReasonCode,
		EffectiveAt: item.EffectiveAt, ExpiresAt: item.ExpiresAt, CompletedAt: item.CompletedAt, CreatedAt: item.CreatedAt,
	}
}

func policyInput(input sohaapi.NetworkAccessPolicyInput) domainnetworkaccess.PolicyInput {
	return domainnetworkaccess.PolicyInput{
		Name: input.Name, Enabled: input.Enabled, Priority: input.Priority, Effect: string(input.Effect),
		Subjects: domainnetworkaccess.PolicySubjects{Users: input.Subjects.Users, Teams: input.Subjects.Teams, Tags: input.Subjects.Tags},
		SiteIDs:  input.SiteIDs, ResourceIDs: input.ResourceIDs, Modes: stringValues(input.Modes),
		DeviceStatuses: stringValues(input.DeviceStatuses), PostureStatuses: stringValues(input.PostureStatuses), AccessProfile: string(input.AccessProfile),
	}
}

func stringValues[T ~string](values []T) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func principal(c *gin.Context) domainidentity.Principal { return apiMiddleware.PrincipalFromContext(c) }

func bindNetworkJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid network access payload")
		return false
	}
	return true
}

func bindNetworkQuery(c *gin.Context, target any) bool {
	if err := c.ShouldBindQuery(target); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid network access query")
		return false
	}
	if raw, exists := c.GetQuery("limit"); exists {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid network access query")
			return false
		}
	}
	return true
}

func respondItems[T any](c *gin.Context, items []T, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func respondItem[T any](c *gin.Context, status int, item T, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, status, item)
}

func respondDelete(c *gin.Context, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
