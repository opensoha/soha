package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxypolicy"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"net/http"
)

type ProxyBundles interface {
	List(context.Context, identity.Principal, string) (api.NetworkProxyBundleList, error)
	Get(context.Context, identity.Principal, string, string) (api.NetworkProxyBundle, error)
	Prepare(context.Context, identity.Principal, string, string, api.NetworkProxyBundleInput) (api.NetworkProxyBundle, error)
	Activate(context.Context, identity.Principal, string, string, api.NetworkProxyBundleActivateInput) (api.NetworkProxyBundle, error)
}
type ProxyPublicationBundlesHandler struct{ service ProxyBundles }

func NewProxyPublicationBundlesHandler(service ProxyBundles) *ProxyPublicationBundlesHandler {
	return &ProxyPublicationBundlesHandler{service: service}
}
func (h *ProxyPublicationBundlesHandler) List(c *gin.Context) {
	out, err := h.service.List(c.Request.Context(), principal(c), c.Param("instanceID"))
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationBundlesHandler) Get(c *gin.Context) {
	out, err := h.service.Get(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("bundleID"))
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationBundlesHandler) Prepare(c *gin.Context) {
	var input api.NetworkProxyBundleInput
	if !bindProxyPublicationJSON(c, &input) {
		return
	}
	id := c.Param("bundleID")
	if id == "" {
		id = uuid.NewSHA1(uuid.NameSpaceOID, []byte(c.Param("instanceID")+":"+input.RequestID)).String()
	}
	out, err := h.service.Prepare(c.Request.Context(), principal(c), c.Param("instanceID"), id, input)
	respondItem(c, http.StatusAccepted, out, publicationError(err))
}
func (h *ProxyPublicationBundlesHandler) Activate(c *gin.Context) {
	var input api.NetworkProxyBundleActivateInput
	if !bindProxyPublicationJSON(c, &input) {
		return
	}
	out, err := h.service.Activate(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("bundleID"), input)
	respondItem(c, http.StatusAccepted, out, publicationError(err))
}

type ProxyRuleService interface {
	Get(context.Context, identity.Principal, string, bool, []proxypolicy.Target) (api.NetworkProxyRules, error)
	Put(context.Context, identity.Principal, string, bool, api.NetworkProxyRulesInput, []proxypolicy.Target) (api.NetworkProxyRules, error)
}
type ProxyRuleTargets interface {
	RuleTargets(context.Context, identity.Principal, string) ([]proxypolicy.Target, error)
}
type ProxyPublicationRulesHandler struct {
	service ProxyRuleService
	targets ProxyRuleTargets
}

func NewProxyPublicationRulesHandler(service ProxyRuleService, targets ProxyRuleTargets) *ProxyPublicationRulesHandler {
	return &ProxyPublicationRulesHandler{service: service, targets: targets}
}
func (h *ProxyPublicationRulesHandler) get(c *gin.Context, personal bool) {
	p := principal(c)
	targets, err := h.targets.RuleTargets(c.Request.Context(), p, c.Param("instanceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	out, err := h.service.Get(c.Request.Context(), p, c.Param("instanceID"), personal, targets)
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationRulesHandler) put(c *gin.Context, personal bool) {
	var input api.NetworkProxyRulesInput
	if !bindProxyPublicationJSON(c, &input) {
		return
	}
	p := principal(c)
	targets, err := h.targets.RuleTargets(c.Request.Context(), p, c.Param("instanceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	out, err := h.service.Put(c.Request.Context(), p, c.Param("instanceID"), personal, input, targets)
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationRulesHandler) Get(c *gin.Context)         { h.get(c, false) }
func (h *ProxyPublicationRulesHandler) Put(c *gin.Context)         { h.put(c, false) }
func (h *ProxyPublicationRulesHandler) GetPersonal(c *gin.Context) { h.get(c, true) }
func (h *ProxyPublicationRulesHandler) PutPersonal(c *gin.Context) { h.put(c, true) }
