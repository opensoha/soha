package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	app "github.com/opensoha/soha/internal/application/networkaccess"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NetworkVPNProfiles interface {
	ListProfiles(context.Context, identity.Principal, domain.VPNDocumentFilter) ([]domain.VPNProfile, error)
	GetProfile(context.Context, identity.Principal, string) (domain.VPNProfile, error)
	SaveProfile(context.Context, identity.Principal, string, int, domain.VPNProfileConfig) (domain.VPNProfile, error)
	PublishProfile(context.Context, identity.Principal, string, int, int) (domain.VPNProfile, error)
	DeleteProfile(context.Context, identity.Principal, string, int) error
	ProfileRevisions(context.Context, identity.Principal, string) ([]domain.VPNRevision[domain.VPNProfileConfig], error)
}

type NetworkVPNPolicies interface {
	ListSelectionPolicies(context.Context, identity.Principal, domain.VPNDocumentFilter) ([]domain.VPNSelectionPolicy, error)
	GetSelectionPolicy(context.Context, identity.Principal, string) (domain.VPNSelectionPolicy, error)
	SaveSelectionPolicy(context.Context, identity.Principal, string, int, domain.VPNSelectionPolicyConfig) (domain.VPNSelectionPolicy, error)
	PublishSelectionPolicy(context.Context, identity.Principal, string, int, int) (domain.VPNSelectionPolicy, error)
	DeleteSelectionPolicy(context.Context, identity.Principal, string, int) error
	SelectionPolicyRevisions(context.Context, identity.Principal, string) ([]domain.VPNRevision[domain.VPNSelectionPolicyConfig], error)
}

type NetworkVPNConnections interface {
	ListConnectionOptions(context.Context, identity.Principal, string) ([]domain.VPNConnectionOption, error)
	CreateConnectionIntent(context.Context, identity.Principal, string, domain.VPNIntentInput) (domain.VPNIntentSecret, error)
}

type NetworkVPNHandler struct {
	profiles    NetworkVPNProfiles
	policies    NetworkVPNPolicies
	connections NetworkVPNConnections
	queries     NetworkVPNQueries
}

func NewNetworkVPNHandler(service *app.VPNService) *NetworkVPNHandler {
	if service == nil {
		return nil
	}
	return &NetworkVPNHandler{profiles: service, policies: service, connections: service, queries: service}
}

func (h *NetworkVPNHandler) ListProfiles(c *gin.Context) {
	var query api.ListNetworkVPNProfilesParams
	if !bindNetworkQuery(c, &query) {
		return
	}
	items, err := h.profiles.ListProfiles(c.Request.Context(), principal(c), domain.VPNDocumentFilter{Search: query.Search, Limit: query.Limit})
	respondItems(c, items, err)
}
func (h *NetworkVPNHandler) GetProfile(c *gin.Context) {
	item, err := h.profiles.GetProfile(c.Request.Context(), principal(c), c.Param("id"))
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) CreateProfile(c *gin.Context) { h.saveProfile(c, "") }
func (h *NetworkVPNHandler) UpdateProfile(c *gin.Context) { h.saveProfile(c, c.Param("id")) }
func (h *NetworkVPNHandler) saveProfile(c *gin.Context, id string) {
	var input api.NetworkVPNProfileInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.profiles.SaveProfile(c.Request.Context(), principal(c), id, input.ExpectedRevision, vpnProfileConfig(input.Configuration))
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	respondItem(c, status, item, err)
}
func (h *NetworkVPNHandler) PublishProfile(c *gin.Context) {
	var input api.NetworkVPNRevisionInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.profiles.PublishProfile(c.Request.Context(), principal(c), c.Param("id"), input.ExpectedRevision, 0)
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) RollbackProfile(c *gin.Context) {
	var input api.NetworkVPNRollbackInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.profiles.PublishProfile(c.Request.Context(), principal(c), c.Param("id"), input.ExpectedRevision, input.TargetRevision)
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) DeleteProfile(c *gin.Context) {
	revision, ok := vpnExpectedRevision(c)
	if !ok {
		return
	}
	respondDelete(c, h.profiles.DeleteProfile(c.Request.Context(), principal(c), c.Param("id"), revision))
}
func (h *NetworkVPNHandler) ProfileRevisions(c *gin.Context) {
	items, err := h.profiles.ProfileRevisions(c.Request.Context(), principal(c), c.Param("id"))
	respondItems(c, items, err)
}

func (h *NetworkVPNHandler) ListPolicies(c *gin.Context) {
	var query api.ListNetworkVPNSelectionPoliciesParams
	if !bindNetworkQuery(c, &query) {
		return
	}
	items, err := h.policies.ListSelectionPolicies(c.Request.Context(), principal(c), domain.VPNDocumentFilter{Search: query.Search, Limit: query.Limit})
	respondItems(c, items, err)
}
func (h *NetworkVPNHandler) GetPolicy(c *gin.Context) {
	item, err := h.policies.GetSelectionPolicy(c.Request.Context(), principal(c), c.Param("id"))
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) CreatePolicy(c *gin.Context) { h.savePolicy(c, "") }
func (h *NetworkVPNHandler) UpdatePolicy(c *gin.Context) { h.savePolicy(c, c.Param("id")) }
func (h *NetworkVPNHandler) savePolicy(c *gin.Context, id string) {
	var input api.NetworkVPNSelectionPolicyInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.SaveSelectionPolicy(c.Request.Context(), principal(c), id, input.ExpectedRevision, vpnSelectionPolicyConfig(input.Configuration))
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	respondItem(c, status, item, err)
}
func (h *NetworkVPNHandler) PublishPolicy(c *gin.Context) {
	var input api.NetworkVPNRevisionInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.PublishSelectionPolicy(c.Request.Context(), principal(c), c.Param("id"), input.ExpectedRevision, 0)
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) RollbackPolicy(c *gin.Context) {
	var input api.NetworkVPNRollbackInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.PublishSelectionPolicy(c.Request.Context(), principal(c), c.Param("id"), input.ExpectedRevision, input.TargetRevision)
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) DeletePolicy(c *gin.Context) {
	revision, ok := vpnExpectedRevision(c)
	if !ok {
		return
	}
	respondDelete(c, h.policies.DeleteSelectionPolicy(c.Request.Context(), principal(c), c.Param("id"), revision))
}
func (h *NetworkVPNHandler) PolicyRevisions(c *gin.Context) {
	items, err := h.policies.SelectionPolicyRevisions(c.Request.Context(), principal(c), c.Param("id"))
	respondItems(c, items, err)
}

func (h *NetworkVPNHandler) ConnectionOptions(c *gin.Context) {
	var query api.ListCurrentNetworkVPNConnectionOptionsParams
	if !bindNetworkQuery(c, &query) {
		return
	}
	items, err := h.connections.ListConnectionOptions(c.Request.Context(), principal(c), query.DeviceID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.JSON(c, http.StatusOK, struct {
		Items []domain.VPNConnectionOption `json:"items"`
		AsOf  time.Time                    `json:"asOf"`
	}{items, time.Now().UTC()})
}
func (h *NetworkVPNHandler) CreateIntent(c *gin.Context) {
	var input api.NetworkVPNConnectionIntentInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.connections.CreateConnectionIntent(c.Request.Context(), principal(c), middleware.AccessContextFromContext(c).SessionID, domain.VPNIntentInput{DeviceID: input.DeviceID, ProfileID: input.ProfileID, Selection: string(input.Selection), GatewayID: input.GatewayID})
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusCreated, item, err)
}

func vpnExpectedRevision(c *gin.Context) (int, bool) {
	value, err := strconv.Atoi(c.Query("expectedRevision"))
	if err != nil || value < 1 {
		writeError(c, apperrors.ErrInvalidArgument)
		return 0, false
	}
	return value, true
}
func vpnProfileConfig(input api.NetworkVPNProfileConfig) domain.VPNProfileConfig {
	return domain.VPNProfileConfig{Name: input.Name, SiteID: input.SiteID, NetworkSpaceID: input.NetworkSpaceID, Mode: string(input.Mode), ResourceIDs: input.ResourceIDs, GatewayIDs: input.GatewayIDs, SelectionPolicyID: input.SelectionPolicyID, AllowManualSelection: input.AllowManualSelection, Enabled: input.Enabled, Assignments: domain.VPNAssignment{UserIDs: input.Assignments.UserIDs, TeamIDs: input.Assignments.TeamIDs, DeviceIDs: input.Assignments.DeviceIDs}}
}
func vpnSelectionPolicyConfig(input api.NetworkVPNSelectionPolicyConfig) domain.VPNSelectionPolicyConfig {
	return domain.VPNSelectionPolicyConfig{Name: input.Name, Strategy: string(input.Strategy), ProviderOrder: input.ProviderOrder, ProviderPreference: string(input.ProviderPreference), MaxLatencyMs: input.MaxLatencyMs, MaxTimeoutPercent: input.MaxTimeoutPercent, MaxSampleAgeSeconds: input.MaxSampleAgeSeconds, MinSamples: input.MinSamples, MissingMeasurements: string(input.MissingMeasurements), MaxAttempts: input.MaxAttempts, RetryCooldownSeconds: input.RetryCooldownSeconds, FailoverOnDisconnect: input.FailoverOnDisconnect, AllowManualFallback: input.AllowManualFallback}
}
