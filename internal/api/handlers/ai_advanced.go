package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appagenticrag "github.com/opensoha/soha/internal/application/agenticrag"
	appaieval "github.com/opensoha/soha/internal/application/aieval"
	appknowledgegraph "github.com/opensoha/soha/internal/application/knowledgegraph"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	appmultiagent "github.com/opensoha/soha/internal/application/multiagent"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type AIAdvancedHandler struct {
	evaluation     *appaieval.AdvancedService
	runs           *appaieval.Service
	memory         *appmemory.Service
	graph          *appknowledgegraph.Service
	multiAgent     *appmultiagent.Service
	retriever      appagenticrag.Retriever
	permissionKeys interface {
		PermissionKeys(context.Context, domainidentity.Principal) ([]string, error)
	}
	knowledgeBases interface {
		GetBase(context.Context, domainidentity.Principal, string) (domainknowledge.KnowledgeBase, error)
	}
	auth           EvaluationAuthorizer
	features       map[string]bool
	strictFeatures bool
}

func NewAIAdvancedHandler(evaluation *appaieval.AdvancedService, runs *appaieval.Service, memory *appmemory.Service, graph *appknowledgegraph.Service, multiAgent *appmultiagent.Service, retriever appagenticrag.Retriever, auth EvaluationAuthorizer, featureSets ...map[string]bool) *AIAdvancedHandler {
	h := &AIAdvancedHandler{evaluation: evaluation, runs: runs, memory: memory, graph: graph, multiAgent: multiAgent, retriever: retriever, auth: auth}
	if access, ok := retriever.(interface {
		GetBase(context.Context, domainidentity.Principal, string) (domainknowledge.KnowledgeBase, error)
	}); ok {
		h.knowledgeBases = access
	}
	if resolver, ok := auth.(interface {
		PermissionKeys(context.Context, domainidentity.Principal) ([]string, error)
	}); ok {
		h.permissionKeys = resolver
	}
	if len(featureSets) > 0 {
		h.features = maps.Clone(featureSets[0])
		h.strictFeatures = true
	}
	return h
}

func (h *AIAdvancedHandler) featureEnabled(key string) bool {
	return h != nil && (!h.strictFeatures || h.features[key])
}

func RegisterAIAdvancedRoutes(group gin.IRoutes, h *AIAdvancedHandler) {
	if h.featureEnabled("evaluation.candidate_executor") {
		group.GET("/ai/evaluations/executor-profiles", h.listExecutorProfiles)
		group.POST("/ai/evaluations/executor-profiles", h.putExecutorProfile)
		group.POST("/ai/evaluations/runs/:runID/execute", h.executeRun)
		group.GET("/ai/evaluations/runs/:runID/attempts", h.listAttempts)
	}
	if h.featureEnabled("evaluation.isolated_replay") {
		group.GET("/ai/evaluations/replays", h.listReplays)
		group.POST("/ai/evaluations/replays", h.putReplay)
	}
	if h.featureEnabled("evaluation.release_gate") {
		group.GET("/ai/evaluations/gate-policies", h.listGatePolicies)
		group.POST("/ai/evaluations/gate-policies", h.putGatePolicy)
		group.GET("/ai/evaluations/gate-decisions", h.listGateDecisions)
		group.POST("/ai/evaluations/gates/evaluate", h.evaluateGate)
	}
	if h.featureEnabled("evaluation.feedback_sampling") {
		group.GET("/ai/evaluations/feedback", h.listFeedback)
		group.POST("/ai/evaluations/feedback", h.putFeedback)
	}

	if h.featureEnabled("memory.long_term") {
		group.GET("/ai/memory/policies", h.listMemoryPolicies)
		group.POST("/ai/memory/policies", h.putMemoryPolicy)
		group.GET("/ai/memory", h.listMemory)
		group.POST("/ai/memory", h.putMemory)
		group.DELETE("/ai/memory/:memoryID", h.deleteMemory)
	}

	if h.featureEnabled("retrieval.graphrag") {
		group.GET("/ai/knowledge-bases/:baseID/graph-revisions", h.listGraphRevisions)
		group.POST("/ai/knowledge-bases/:baseID/graph-revisions", h.putGraphRevision)
		group.POST("/ai/knowledge-bases/:baseID/graph-revisions/:revisionID/publish", h.publishGraphRevision)
		group.POST("/ai/knowledge-bases/:baseID/graph-revisions/:revisionID/query", h.queryGraphRevision)
	}
	if h.featureEnabled("retrieval.agentic") {
		group.POST("/ai/knowledge/agentic-search", h.agenticSearch)
	}

	if h.featureEnabled("agent.multi_agent") {
		group.GET("/ai/agent-runs/multi-agent", h.listMultiAgent)
		group.POST("/ai/agent-runs/multi-agent", h.createMultiAgent)
		group.POST("/ai/agent-runs/multi-agent/:planID/subtasks/:subtaskID/complete", h.completeMultiAgentSubtask)
		group.POST("/ai/agent-runs/multi-agent/:planID/cancel", h.cancelMultiAgent)
	}
}

func (h *AIAdvancedHandler) allowed(c *gin.Context, permissions ...string) bool {
	if h == nil || h.auth == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "advanced AI service unavailable")
		return false
	}
	err := h.auth.AuthorizeAny(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), permissions...)
	if err == nil {
		return true
	}
	if errors.Is(err, apperrors.ErrAccessDenied) {
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "advanced AI access denied")
	} else {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "advanced AI authorization unavailable")
	}
	return false
}

func (h *AIAdvancedHandler) listExecutorProfiles(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsView, appaccess.PermAIEvaluationsExecute) {
		return
	}
	items, err := h.evaluation.ListExecutorProfiles(c)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) putExecutorProfile(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsExecute) {
		return
	}
	var input appaieval.ExecutorProfile
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid executor profile"))
		return
	}
	if err := h.evaluation.PutExecutorProfile(c, input); err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, input)
}
func (h *AIAdvancedHandler) executeRun(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsExecute) {
		return
	}
	var input struct {
		Profile           appaieval.ExecutorProfile `json:"profile"`
		ExecutorProfileID string                    `json:"executorProfileId"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid execution request"))
		return
	}
	profile, err := h.resolveExecutorProfile(c, input.ExecutorProfileID, input.Profile)
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	item, err := h.evaluation.ExecuteRun(c, apiMiddleware.PrincipalFromContext(c), c.Param("runID"), profile)
	writeAdvancedItem(c, http.StatusAccepted, item, err)
}
func (h *AIAdvancedHandler) listAttempts(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsView, appaccess.PermAIEvaluationsExecute) {
		return
	}
	items, err := h.evaluation.ListAttempts(c, c.Param("runID"))
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) listReplays(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsView, appaccess.PermAIEvaluationsExecute) {
		return
	}
	items, err := h.evaluation.ListReplayPlans(c)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) putReplay(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsExecute) {
		return
	}
	var input struct {
		appaieval.ReplayPlan
		BaselineRunID     string `json:"baselineRunId"`
		CandidateRunID    string `json:"candidateRunId"`
		ExecutorProfileID string `json:"executorProfileId"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid replay plan"))
		return
	}
	plan := input.ReplayPlan
	if len(plan.SourceTraceRefs) == 0 && input.BaselineRunID != "" {
		plan.SourceTraceRefs = []string{"evaluation-run:" + strings.TrimSpace(input.BaselineRunID)}
		plan.CandidateRefs = map[string]string{"evaluationRun": strings.TrimSpace(input.CandidateRunID)}
		plan.ReadOnly = true
		profile, err := h.resolveExecutorProfile(c, input.ExecutorProfileID, plan.Profile)
		if err != nil {
			writeAdvancedError(c, err)
			return
		}
		plan.Profile = profile
	}
	if err := h.evaluation.PutReplayPlan(c, plan); err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, plan)
}
func (h *AIAdvancedHandler) listGatePolicies(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsView, appaccess.PermAIEvaluationsGatesManage) {
		return
	}
	items, err := h.evaluation.ListGatePolicies(c)
	writeAdvancedItems(c, items, err)
}

func (h *AIAdvancedHandler) resolveExecutorProfile(ctx context.Context, id string, inline appaieval.ExecutorProfile) (appaieval.ExecutorProfile, error) {
	if inline.ID != "" {
		return inline, nil
	}
	id = strings.TrimSpace(id)
	items, err := h.evaluation.ListExecutorProfiles(ctx)
	if err != nil {
		return appaieval.ExecutorProfile{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return appaieval.ExecutorProfile{}, fmt.Errorf("%w: executor profile %q", appaieval.ErrNotFound, id)
}

func (h *AIAdvancedHandler) resolveGatePolicy(ctx context.Context, id string) (appaieval.GatePolicy, error) {
	id = strings.TrimSpace(id)
	items, err := h.evaluation.ListGatePolicies(ctx)
	if err != nil {
		return appaieval.GatePolicy{}, err
	}
	var selected appaieval.GatePolicy
	for _, item := range items {
		if item.ID == id && (selected.ID == "" || item.Version > selected.Version) {
			selected = item
		}
	}
	if selected.ID == "" {
		return appaieval.GatePolicy{}, fmt.Errorf("%w: gate policy %q", appaieval.ErrNotFound, id)
	}
	return selected, nil
}
func (h *AIAdvancedHandler) putGatePolicy(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsGatesManage) {
		return
	}
	var input struct {
		appaieval.GatePolicy
		Name      string  `json:"name"`
		Metric    string  `json:"metric"`
		Threshold float64 `json:"threshold"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid gate policy"))
		return
	}
	policy := input.GatePolicy
	if len(policy.MinimumScores) == 0 && strings.TrimSpace(input.Metric) != "" {
		policy.MinimumScores = map[string]float64{strings.TrimSpace(input.Metric): input.Threshold}
		policy.Enabled = true
	}
	if err := h.evaluation.PutGatePolicy(c, policy); err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, policy)
}
func (h *AIAdvancedHandler) listGateDecisions(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsView, appaccess.PermAIEvaluationsGatesManage) {
		return
	}
	items, err := h.evaluation.ListGateDecisions(c)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) evaluateGate(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsGatesManage) {
		return
	}
	var input struct {
		ID             string               `json:"id"`
		Policy         appaieval.GatePolicy `json:"policy"`
		BaselineRunID  string               `json:"baselineRunId"`
		CandidateRunID string               `json:"candidateRunId"`
		PolicyID       string               `json:"policyId"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid gate request"))
		return
	}
	policy := input.Policy
	var err error
	if policy.ID == "" {
		policy, err = h.resolveGatePolicy(c, input.PolicyID)
		if err != nil {
			writeAdvancedError(c, err)
			return
		}
	}
	baseline, err := h.runs.GetRun(c, input.BaselineRunID)
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	candidate, err := h.runs.GetRun(c, input.CandidateRunID)
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	if input.ID == "" {
		input.ID = "gate-" + strings.TrimSpace(input.CandidateRunID) + "-" + policy.Version
	}
	item, err := h.evaluation.EvaluateGate(c, input.ID, policy, baseline, candidate)
	writeAdvancedItem(c, http.StatusOK, item, err)
}
func (h *AIAdvancedHandler) listFeedback(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsView, appaccess.PermAIEvaluationsFeedbackCurate) {
		return
	}
	items, err := h.evaluation.ListFeedback(c)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) putFeedback(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEvaluationsFeedbackCurate) {
		return
	}
	var input struct {
		appaieval.FeedbackSample
		Disposition string `json:"disposition"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid feedback sample"))
		return
	}
	sample := input.FeedbackSample
	if sample.Decision == "" {
		sample.Decision = strings.TrimSpace(input.Disposition)
	}
	if sample.ScopeHash == "" {
		sum := sha256.Sum256([]byte(apiMiddleware.PrincipalFromContext(c).UserID))
		sample.ScopeHash = "sha256:" + hex.EncodeToString(sum[:])
	}
	if err := h.evaluation.PutFeedback(c, sample); err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, sample)
}

func (h *AIAdvancedHandler) listMemoryPolicies(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMemoryView, appaccess.PermAIMemoryManage) {
		return
	}
	items, err := h.memory.ListPolicies(c)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) putMemoryPolicy(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMemoryManage) {
		return
	}
	var input struct {
		appmemory.Policy
		Name        string `json:"name"`
		ConsentMode string `json:"consentMode"`
		TTLDays     int    `json:"ttlDays"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid memory policy"))
		return
	}
	policy := input.Policy
	if policy.Version == "" && input.TTLDays > 0 {
		policy.Version = "v1"
		policy.OwnerTypes = []string{"user"}
		policy.DefaultTTL = time.Duration(input.TTLDays) * 24 * time.Hour
		policy.MaximumTTL = policy.DefaultTTL
		policy.MinimumConfidence = 0.5
		policy.ExplicitWriteOnly = input.ConsentMode == "explicit"
		policy.Enabled = input.ConsentMode != "disabled"
	}
	if err := h.memory.PutPolicy(c, policy); err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, policy)
}
func (h *AIAdvancedHandler) listMemory(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMemoryView, appaccess.PermAIMemoryManage) {
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	ownerType, ownerID := strings.TrimSpace(c.Query("ownerType")), strings.TrimSpace(c.Query("ownerId"))
	if ownerType == "" {
		ownerType = "user"
	}
	if ownerID == "" {
		ownerID = principal.UserID
	}
	if ownerType == "user" && ownerID != principal.UserID && !h.allowed(c, appaccess.PermAIMemoryManage) {
		return
	}
	items, err := h.memory.ListRecords(c, ownerType, ownerID)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) putMemory(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMemoryManage) {
		return
	}
	var input struct {
		Record        appmemory.Record `json:"record"`
		PolicyID      string           `json:"policyId"`
		PolicyVersion string           `json:"policyVersion"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid memory record"))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if input.Record.OwnerType == "user" && input.Record.OwnerID != principal.UserID {
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "cannot write another user's memory")
		return
	}
	policy, err := h.memory.GetPolicy(c, input.PolicyID, input.PolicyVersion)
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	item, err := h.memory.PutRecord(c, input.Record, policy)
	writeAdvancedItem(c, http.StatusCreated, item, err)
}
func (h *AIAdvancedHandler) deleteMemory(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMemoryManage) {
		return
	}
	item, err := h.memory.GetRecord(c, c.Param("memoryID"))
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if item.OwnerType == "user" && item.OwnerID != principal.UserID {
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "cannot delete another user's memory")
		return
	}
	if err := h.memory.DeleteRecord(c, item.ID); err != nil {
		writeAdvancedError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AIAdvancedHandler) listGraphRevisions(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIKnowledgeView, appaccess.PermAIKnowledgeGraphManage) {
		return
	}
	if !h.requireKnowledgeBaseAccess(c, c.Param("baseID")) {
		return
	}
	items, err := h.graph.List(c, c.Param("baseID"))
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) putGraphRevision(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIKnowledgeGraphManage) {
		return
	}
	var input appknowledgegraph.Revision
	if c.ShouldBindJSON(&input) != nil || (input.KnowledgeBaseID != "" && input.KnowledgeBaseID != c.Param("baseID")) {
		writeAdvancedError(c, errors.New("invalid graph revision"))
		return
	}
	input.KnowledgeBaseID = c.Param("baseID")
	if !h.requireKnowledgeBaseAccess(c, input.KnowledgeBaseID) {
		return
	}
	if err := h.graph.PutRevision(c, input); err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, input)
}
func (h *AIAdvancedHandler) publishGraphRevision(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIKnowledgeGraphManage) {
		return
	}
	revision, err := h.graph.GetRevision(c, c.Param("revisionID"))
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	if revision.KnowledgeBaseID != c.Param("baseID") {
		writeAdvancedError(c, errors.New("graph revision base mismatch"))
		return
	}
	if !h.requireKnowledgeBaseAccess(c, revision.KnowledgeBaseID) {
		return
	}
	item, err := h.graph.Publish(c, revision.ID)
	writeAdvancedItem(c, http.StatusOK, item, err)
}
func (h *AIAdvancedHandler) queryGraphRevision(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIKnowledgeView) {
		return
	}
	var input struct {
		Query string `json:"query"`
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid graph query"))
		return
	}
	revision, err := h.graph.GetRevision(c, c.Param("revisionID"))
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	if revision.KnowledgeBaseID != c.Param("baseID") {
		writeAdvancedError(c, errors.New("graph revision base mismatch"))
		return
	}
	if !h.requireKnowledgeBaseAccess(c, revision.KnowledgeBaseID) {
		return
	}
	item, err := h.graph.Query(c, revision.ID, input.Query, input.Mode, input.Limit)
	writeAdvancedItem(c, http.StatusOK, item, err)
}

func (h *AIAdvancedHandler) requireKnowledgeBaseAccess(c *gin.Context, baseID string) bool {
	if h == nil || h.knowledgeBases == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "knowledge access validation unavailable")
		return false
	}
	_, err := h.knowledgeBases.GetBase(c, apiMiddleware.PrincipalFromContext(c), strings.TrimSpace(baseID))
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, domainknowledge.ErrBaseNotFound):
		apiresponse.Error(c, http.StatusNotFound, "not_found", "knowledge base not found")
	case errors.Is(err, apperrors.ErrAccessDenied):
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "knowledge base access denied")
	default:
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "knowledge base access validation failed")
	}
	return false
}

type boundedQueryPlanner struct{ rounds [][]string }

func (p boundedQueryPlanner) Plan(_ context.Context, _ string, round int, _ []domainknowledge.Citation) (appagenticrag.QueryPlan, error) {
	if round < 1 || round > len(p.rounds) {
		return appagenticrag.QueryPlan{StopReason: "plan_complete"}, nil
	}
	return appagenticrag.QueryPlan{Queries: p.rounds[round-1]}, nil
}

func (h *AIAdvancedHandler) agenticSearch(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIKnowledgeView) {
		return
	}
	var input struct {
		KnowledgeBaseIDs []string   `json:"knowledgeBaseIds"`
		Goal             string     `json:"goal"`
		RoundQueries     [][]string `json:"roundQueries"`
		TopK             int        `json:"topK"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.RoundQueries) == 0 || len(input.RoundQueries) > 5 {
		writeAdvancedError(c, errors.New("invalid agentic search plan"))
		return
	}
	service, err := appagenticrag.NewService(h.retriever, boundedQueryPlanner{rounds: input.RoundQueries})
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	item, err := service.Execute(c, apiMiddleware.PrincipalFromContext(c), input.KnowledgeBaseIDs, input.Goal, len(input.RoundQueries), input.TopK)
	writeAdvancedItem(c, http.StatusOK, item, err)
}

func (h *AIAdvancedHandler) listMultiAgent(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMultiAgentRun) {
		return
	}
	items, err := h.multiAgent.List(c)
	writeAdvancedItems(c, items, err)
}
func (h *AIAdvancedHandler) createMultiAgent(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMultiAgentRun) {
		return
	}
	var input appmultiagent.Plan
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid multi-agent plan"))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if h.permissionKeys == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "multi-agent permission resolution unavailable")
		return
	}
	grantedPermissions, err := h.permissionKeys.PermissionKeys(c, principal)
	if err != nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "multi-agent permission resolution unavailable")
		return
	}
	item, err := h.multiAgent.Create(c, input, grantedPermissions)
	writeAdvancedItem(c, http.StatusAccepted, item, err)
}
func (h *AIAdvancedHandler) completeMultiAgentSubtask(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMultiAgentRun) {
		return
	}
	var input struct {
		OutputRef string `json:"outputRef"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeAdvancedError(c, errors.New("invalid subtask completion"))
		return
	}
	item, err := h.multiAgent.CompleteSubtask(c, c.Param("planID"), c.Param("subtaskID"), input.OutputRef)
	writeAdvancedItem(c, http.StatusOK, item, err)
}
func (h *AIAdvancedHandler) cancelMultiAgent(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIMultiAgentRun) {
		return
	}
	item, err := h.multiAgent.Cancel(c, c.Param("planID"))
	writeAdvancedItem(c, http.StatusOK, item, err)
}

func writeAdvancedItem(c *gin.Context, status int, item any, err error) {
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Item(c, status, item)
}
func writeAdvancedItems(c *gin.Context, items any, err error) {
	if err != nil {
		writeAdvancedError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func writeAdvancedError(c *gin.Context, err error) {
	if errors.Is(err, appaieval.ErrNotFound) || errors.Is(err, appmemory.ErrNotFound) || errors.Is(err, appknowledgegraph.ErrNotFound) || errors.Is(err, appmultiagent.ErrNotFound) {
		apiresponse.Error(c, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, appaieval.ErrConflict) || strings.Contains(err.Error(), "terminal") {
		apiresponse.Error(c, http.StatusConflict, "conflict", err.Error())
		return
	}
	if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "exceeds") || strings.Contains(err.Error(), "outside") || strings.Contains(err.Error(), "incomplete") || strings.Contains(err.Error(), "mismatch") {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "advanced AI operation failed (reference "+strconv.Itoa(http.StatusInternalServerError)+")")
}
