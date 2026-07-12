package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func (s *Service) Capabilities(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ManifestRequest) (domainaigateway.Manifest, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayView); err != nil {
		_ = s.recordManifestAudit(ctx, principal, input, "deny", err.Error(), 0, len(s.gatewayTools()))
		return domainaigateway.Manifest{}, err
	}
	permissionKeys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}

	tools, deniedTools := filterTools(s.gatewayTools(), permissionKeys)
	resources, deniedResources := filterResources(s.gatewayResources(), permissionKeys)
	prompts, deniedPrompts := filterPrompts(s.gatewayPrompts(), permissionKeys)
	skills, deniedSkills := filterSkills(s.gatewaySkills(), permissionKeys)
	tools, grantDenied, err := s.filterToolsByGrants(ctx, principal, input.AIClientID, tools, permissionKeys)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	deniedTools += grantDenied
	tools, policyDeniedTools, err := s.filterToolsByAccessPolicies(ctx, principal, input.AIClientID, input.SkillID, tools)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	deniedTools += policyDeniedTools
	skills, policyDeniedSkills, err := s.filterSkillsByAccessPolicies(ctx, principal, input.AIClientID, skills)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	deniedSkills += policyDeniedSkills
	bindings, err := s.activeSkillBindings(ctx, principal, input.AIClientID)
	if err != nil {
		return domainaigateway.Manifest{}, err
	}
	skills, bindingDeniedSkills := filterSkillsByBindingsWithSkills(skills, bindings, s.gatewaySkills())
	deniedSkills += bindingDeniedSkills
	tools, bindingDeniedTools := filterToolsBySkillBindingsWithSkills(tools, bindings, input.SkillID, s.gatewaySkills())
	deniedTools += bindingDeniedTools
	resources, bindingDeniedResources := filterResourcesBySkillBindingsWithCapabilities(resources, bindings, input.SkillID, s.gatewayResourceCapabilityRefs(), s.gatewaySkills())
	deniedResources += bindingDeniedResources
	prompts, bindingDeniedPrompts := filterPromptsBySkillBindingsWithCapabilities(prompts, bindings, input.SkillID, s.gatewayResources(), s.gatewayResourceCapabilityRefs(), s.gatewaySkills())
	deniedPrompts += bindingDeniedPrompts
	deniedCount := deniedTools + deniedResources + deniedPrompts + deniedSkills

	manifest := domainaigateway.Manifest{
		Name:           "soha AI Gateway",
		Version:        manifestVersion,
		GeneratedAt:    time.Now().UTC(),
		Principal:      principal,
		Caller:         callerContext(input),
		PermissionKeys: permissionKeys,
		Tools:          tools,
		Resources:      resources,
		Prompts:        prompts,
		Skills:         skills,
		Summary: domainaigateway.ManifestSummary{
			ToolCount:     len(tools),
			ResourceCount: len(resources),
			PromptCount:   len(prompts),
			SkillCount:    len(skills),
			DeniedCount:   deniedCount,
		},
	}
	_ = s.recordManifestAudit(ctx, principal, input, "success", "listed AI Gateway capabilities", len(tools), deniedCount)
	return manifest, nil
}
func (s *Service) gatewayRegistry() *capabilityRegistry {
	if s != nil && s.registry != nil {
		return s.registry
	}
	return newDefaultCapabilityRegistry()
}
func (s *Service) gatewayTools() []domainaigateway.ToolCapability {
	return s.gatewayRegistry().Tools()
}
func (s *Service) gatewayResources() []domainaigateway.ResourceCapability {
	return s.gatewayRegistry().Resources()
}
func (s *Service) gatewayPrompts() []domainaigateway.PromptCapability {
	return s.gatewayRegistry().Prompts()
}
func (s *Service) gatewaySkills() []domainaigateway.SkillCapability {
	return s.gatewayRegistry().Skills()
}
func (s *Service) gatewayResourceCapabilityRefs() []ResourceCapabilityRefs {
	return s.gatewayRegistry().ResourceCapabilityRefs()
}
func (s *Service) resourceToolRefs(name string) []string {
	return s.gatewayRegistry().ResourceToolRefs(name)
}
func (s *Service) resourcePromptRefs(name string) []string {
	return s.gatewayRegistry().ResourcePromptRefs(name)
}
func (s *Service) resourceSkillRefs(name string) []string {
	return s.gatewayRegistry().ResourceSkillRefs(name)
}
func (s *Service) toolByName(name string) (domainaigateway.ToolCapability, bool) {
	return s.gatewayRegistry().ToolByName(name)
}
func (s *Service) resourceByName(name string) (domainaigateway.ResourceCapability, bool) {
	return s.gatewayRegistry().ResourceByName(name)
}
func (s *Service) promptByName(name string) (domainaigateway.PromptCapability, bool) {
	return s.gatewayRegistry().PromptByName(name)
}
func (s *Service) skillByID(id string) (domainaigateway.SkillCapability, bool) {
	return s.gatewayRegistry().SkillByID(id)
}
func (s *Service) recordManifestAudit(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ManifestRequest, result, summary string, allowedCount, deniedCount int) error {
	if s == nil || s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ResourceKind:  "AIGatewayManifest",
		Action:        "ai_gateway.capabilities",
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"aiClientId":    strings.TrimSpace(input.AIClientID),
			"aiClientName":  strings.TrimSpace(input.AIClientName),
			"skillId":       strings.TrimSpace(input.SkillID),
			"source":        firstNonEmpty(strings.TrimSpace(input.Source), meta.Source),
			"allowedCount":  allowedCount,
			"deniedCount":   deniedCount,
			"manifest":      manifestVersion,
			"requestClient": meta.UserAgent,
		},
	})
}
func mapInput(input map[string]any, out any) error {
	if input == nil {
		input = map[string]any{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("%w: invalid tool input", apperrors.ErrInvalidArgument)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: invalid tool input: %v", apperrors.ErrInvalidArgument, err)
	}
	return nil
}
func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{}
	}
}
func stringSliceValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}
func sliceLen(value any) int {
	switch typed := value.(type) {
	case []domainresource.PodView:
		return len(typed)
	case []domainresource.DeploymentView:
		return len(typed)
	case []domainresource.ServiceView:
		return len(typed)
	case []domainresource.ClusterEventView:
		return len(typed)
	case []domaindelivery.ExecutionLog:
		return len(typed)
	case []domaindelivery.ExecutionTask:
		return len(typed)
	case []domaindelivery.ExecutionArtifact:
		return len(typed)
	case []any:
		return len(typed)
	default:
		return 0
	}
}
func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
func normalizeGatewayResourceURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const legacyPrefix = "soha://resource/"
	value = strings.TrimPrefix(value, legacyPrefix)
	value = strings.TrimPrefix(value, "resource/")
	if strings.HasPrefix(value, "soha://") {
		return value
	}
	if strings.Contains(value, "/") {
		return "soha://" + strings.TrimPrefix(value, "/")
	}
	return value
}
func marshalGatewayDocument(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("%w: failed to render AI Gateway manifest document", apperrors.ErrInvalidArgument)
	}
	return string(raw), nil
}
func gatewayResourceDocumentWithCapabilities(resource domainaigateway.ResourceCapability, input domainaigateway.ResourceReadRequest, bindings []domainaigateway.SkillBinding, skillID string, toolRefs []string, promptRefs []string, skillRefs []string, tools []domainaigateway.ToolCapability, prompts []domainaigateway.PromptCapability, skills []domainaigateway.SkillCapability) map[string]any {
	toolRefs = filterToolRefsBySkillBindingsWithSkills(toolRefs, bindings, skillID, skills)
	resourceRefs := ResourceCapabilityRefs{Resource: resource.Name, Tools: toolRefs, Prompts: promptRefs, Skills: skillRefs}
	promptRefs = filterPromptRefsBySkillBindingsWithResourceRefs(promptRefs, bindings, skillID, resourceRefs, skills)
	skillRefs = filterSkillRefsByBindingsWithSkills(skillRefs, bindings, skillID, skills)
	return map[string]any{
		"uri":                    resource.Name,
		"name":                   resource.Name,
		"description":            resource.Description,
		"manifestVersion":        manifestVersion,
		"requiredPermissionKeys": append([]string(nil), resource.PermissionKeys...),
		"requiredScopes":         append([]string(nil), resource.RequiredScopes...),
		"requestedContext":       sanitizeGatewayMap(input.Context),
		"relatedTools":           compactToolCapabilitiesFrom(toolRefs, tools),
		"relatedPrompts":         compactPromptCapabilitiesFrom(promptRefs, prompts),
		"relatedSkills":          compactSkillCapabilitiesForBindingsWithSkills(skillRefs, bindings, skillID, skills),
		"recommendedPromptNames": promptRefs,
		"governance": map[string]any{
			"readAction":     "ai_gateway.resource.read",
			"riskLevel":      domainaigateway.RiskLevelRead,
			"permissionGate": appaccess.PermAIGatewayInvoke,
			"auditBoundary":  "backend",
		},
	}
}
func gatewayPromptMessages(prompt domainaigateway.PromptCapability, input domainaigateway.PromptGetRequest, skill *domainaigateway.SkillCapability) []domainaigateway.PromptMessage {
	context := sanitizeGatewayMap(mergeAnyMaps(input.Context, input.Arguments))
	contextText, _ := marshalGatewayDocument(context)
	content := strings.Join(compactStrings(
		"You are operating through soha AI Gateway. Use only capabilities visible to the current identity and keep all mutations behind the owning soha service, risk policy, approval, and durable task boundaries.",
		"Prompt: "+prompt.Name,
		"Purpose: "+prompt.Description,
		gatewayPromptSkillSection(skill),
		gatewayPromptContextSection(contextText),
		gatewayPromptInstruction(prompt.Name),
	), "\n\n")
	return []domainaigateway.PromptMessage{{Role: "user", Content: content}}
}
func gatewayPromptSkillSection(skill *domainaigateway.SkillCapability) string {
	if skill == nil {
		return "Skill context: none supplied. Infer the workflow from the requested prompt and visible Gateway manifest only."
	}
	refs := append([]string(nil), skill.CapabilityRefs...)
	sort.Strings(refs)
	return "Skill context:\n" +
		"- id: " + skill.ID + "\n" +
		"- name: " + skill.Name + "\n" +
		"- category: " + skill.Category + "\n" +
		"- description: " + skill.Description + "\n" +
		"- capabilityRefs: " + strings.Join(refs, ", ")
}
func gatewayPromptContextSection(contextText string) string {
	if strings.TrimSpace(contextText) == "" || strings.TrimSpace(contextText) == "{}" {
		return "Current context: {}"
	}
	return "Current context, redacted by Gateway:\n" + contextText
}
func gatewayPromptInstruction(name string) string {
	switch strings.TrimSpace(name) {
	case "soha.delivery.plan_release":
		return strings.Join([]string{
			"Release planning workflow:",
			"1. Read application detail, environment bindings, build sources, release targets, workflow templates, approval nodes, recent bundles, and execution tasks before proposing an action.",
			"2. Prefer delivery.release_context.diff for candidate promotion evidence and delivery.rollback.context for rollback evidence.",
			"3. Summarize readiness, blockers, blast radius, required approvals, rollback criteria, and exact next tool call inputs.",
			"4. Do not trigger build, deploy, workflow, verify, or rollback actions unless the user explicitly asks and the Gateway response permits it.",
		}, "\n")
	case "soha.k8s.diagnose_workload":
		return strings.Join([]string{
			"Kubernetes diagnosis workflow:",
			"1. Stay read-only. Use scoped cluster and namespace context and do not request raw kubeconfig or raw Kubernetes objects.",
			"2. Collect pod detail, pod logs, deployment rollout status, deployment events, service backends, route context, storage context, node detail, and cluster events as needed.",
			"3. Separate observed evidence from hypotheses and include capabilityWarnings when Gateway API or agent-backed data is unavailable.",
			"4. Return likely causes, confidence, immediate checks, and safe remediation options, but leave mutations to explicit soha tools and Gateway approval guardrails.",
		}, "\n")
	default:
		return "Use the visible Gateway manifest to gather evidence first, keep outputs redacted, and state any missing scope or permission before suggesting a next action."
	}
}
func compactToolCapabilitiesFrom(names []string, tools []domainaigateway.ToolCapability) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		tool, ok := toolByNameFrom(name, tools)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":             tool.Name,
			"description":      tool.Description,
			"riskLevel":        tool.RiskLevel,
			"requiredScopes":   tool.RequiredScopes,
			"permissionKeys":   tool.PermissionKeys,
			"requiresApproval": tool.RequiresApproval,
		})
	}
	return out
}
func compactPromptCapabilitiesFrom(names []string, prompts []domainaigateway.PromptCapability) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		prompt, ok := promptByNameFrom(name, prompts)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":           prompt.Name,
			"description":    prompt.Description,
			"requiredScopes": prompt.RequiredScopes,
			"permissionKeys": prompt.PermissionKeys,
		})
	}
	return out
}
func compactSkillCapabilitiesFrom(ids []string, skills []domainaigateway.SkillCapability) []map[string]any {
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		skill, ok := skillByIDFrom(id, skills)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"id":             skill.ID,
			"name":           skill.Name,
			"category":       skill.Category,
			"description":    skill.Description,
			"capabilityRefs": skill.CapabilityRefs,
			"requiredScopes": skill.RequiredScopes,
			"permissionKeys": skill.PermissionKeys,
		})
	}
	return out
}
func compactSkillCapabilitiesForBindingsWithSkills(ids []string, bindings []domainaigateway.SkillBinding, skillID string, skills []domainaigateway.SkillCapability) []map[string]any {
	if len(bindings) == 0 {
		return compactSkillCapabilitiesFrom(ids, skills)
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		skill, ok := skillByIDFrom(id, skills)
		if !ok {
			continue
		}
		skill = narrowSkillCapabilityByBindingsWithSkills(skill, bindings, skillID, skills)
		if len(skill.CapabilityRefs) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"id":             skill.ID,
			"name":           skill.Name,
			"category":       skill.Category,
			"description":    skill.Description,
			"capabilityRefs": skill.CapabilityRefs,
			"requiredScopes": skill.RequiredScopes,
			"permissionKeys": skill.PermissionKeys,
		})
	}
	return out
}
func narrowSkillCapabilityByBindingsWithSkills(skill domainaigateway.SkillCapability, bindings []domainaigateway.SkillBinding, skillID string, knownSkills []domainaigateway.SkillCapability) domainaigateway.SkillCapability {
	if len(bindings) == 0 {
		return skill
	}
	refs, ok := skillBindingRefsWithSkills(bindings, skillID, knownSkills)[skill.ID]
	if !ok {
		skill.CapabilityRefs = nil
		return skill
	}
	if len(refs) > 0 {
		skill.CapabilityRefs = intersectStringSlices(skill.CapabilityRefs, refs)
	}
	return skill
}
func toolByNameFrom(name string, tools []domainaigateway.ToolCapability) (domainaigateway.ToolCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range tools {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.ToolCapability{}, false
}
func promptByNameFrom(name string, prompts []domainaigateway.PromptCapability) (domainaigateway.PromptCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range prompts {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.PromptCapability{}, false
}
func sortedGatewayMapKeys(values map[string]any) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
