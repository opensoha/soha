package aigateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func enforceGatewayRedactionPolicyCondition(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, toolInput map[string]any) (map[string]any, gatewayRedactionAuditSummary, error) {
	values := gatewayConditionValues(policy.Conditions, "redactionPolicy", "redaction", "sensitiveDataRedaction")
	rules := gatewayRedactionRules(values, tool, "input")
	if len(rules) == 0 {
		return toolInput, gatewayRedactionAuditSummary{}, nil
	}
	out := toolInput
	var summary gatewayRedactionAuditSummary
	for _, rule := range rules {
		ruleSummary := gatewayRedactionAuditSummaryForValue(out, rule, "input")
		ruleSummary.PolicyIDs = append(ruleSummary.PolicyIDs, strings.TrimSpace(policy.ID))
		summary.merge(ruleSummary)
		if rule.DenySensitive {
			if ruleSummary.TotalMatches > 0 {
				return out, summary, fmt.Errorf("%w: AI Gateway redaction policy %s rejected sensitive input for %s", apperrors.ErrAccessDenied, strings.TrimSpace(policy.ID), tool.Name)
			}
			continue
		}
		if rule.SanitizeSensitive {
			out = applyGatewayRedactionRule(out, rule)
		}
	}
	return out, summary, nil
}
func enforceGatewayOutputRedactionPolicyCondition(policy domainaigateway.AccessPolicy, tool domainaigateway.ToolCapability, output any) (any, gatewayRedactionAuditSummary, error) {
	rules := gatewayOutputRedactionRules(policy.Conditions, tool)
	if len(rules) == 0 {
		return output, gatewayRedactionAuditSummary{}, nil
	}
	out := gatewayRedactionSerializableValue(output)
	var summary gatewayRedactionAuditSummary
	for _, rule := range rules {
		ruleSummary := gatewayRedactionAuditSummaryForValue(out, rule, "output")
		ruleSummary.PolicyIDs = append(ruleSummary.PolicyIDs, strings.TrimSpace(policy.ID))
		summary.merge(ruleSummary)
		if rule.DenySensitive {
			if ruleSummary.TotalMatches > 0 {
				return out, summary, fmt.Errorf("%w: AI Gateway output redaction policy %s rejected sensitive output for %s", apperrors.ErrAccessDenied, strings.TrimSpace(policy.ID), tool.Name)
			}
			continue
		}
		if rule.SanitizeSensitive {
			out = applyGatewayRedactionValue(out, rule, "")
		}
	}
	return out, summary, nil
}
func gatewayOutputRedactionRules(conditions map[string]any, tool domainaigateway.ToolCapability) []gatewayRedactionRule {
	out := make([]gatewayRedactionRule, 0)
	values := gatewayConditionValues(conditions, "redactionPolicy", "redaction", "sensitiveDataRedaction")
	out = append(out, gatewayRedactionRules(values, tool, "output")...)
	for _, alias := range []string{"outputRedactionPolicy", "outputRedaction", "responseRedactionPolicy", "responseRedaction"} {
		raw, ok := gatewayConditionRaw(conditions, alias)
		if !ok {
			continue
		}
		values := mapValue(raw)
		if len(values) == 0 {
			continue
		}
		if _, ok := gatewayConditionRaw(values, "target"); !ok {
			values["target"] = "output"
		}
		out = append(out, gatewayRedactionRules(values, tool, "output")...)
	}
	return out
}

type gatewayRedactionRule struct {
	Mode              string
	Target            string
	Fields            []string
	AllowFields       []string
	ValuePatterns     []string
	SecretTypes       []string
	Replacement       string
	PreserveFormat    bool
	DenySensitive     bool
	SanitizeSensitive bool
}

type gatewayRedactionAuditSummary struct {
	TotalMatches            int
	FieldMatches            int
	SensitiveKeyMatches     int
	SensitiveTextMatches    int
	ValuePatternMatches     int
	SecretClassifierMatches int
	StructuredSecretMatches int
	Targets                 []string
	FieldPaths              []string
	MatchTypes              []string
	Classifiers             []string
	PolicyIDs               []string
}

func (summary gatewayRedactionAuditSummary) empty() bool {
	return summary.TotalMatches == 0
}
func (summary *gatewayRedactionAuditSummary) merge(other gatewayRedactionAuditSummary) {
	if summary == nil || other.empty() {
		return
	}
	summary.TotalMatches += other.TotalMatches
	summary.FieldMatches += other.FieldMatches
	summary.SensitiveKeyMatches += other.SensitiveKeyMatches
	summary.SensitiveTextMatches += other.SensitiveTextMatches
	summary.ValuePatternMatches += other.ValuePatternMatches
	summary.SecretClassifierMatches += other.SecretClassifierMatches
	summary.StructuredSecretMatches += other.StructuredSecretMatches
	summary.Targets = gatewayAppendUniqueStrings(summary.Targets, other.Targets...)
	summary.FieldPaths = gatewayAppendUniqueStrings(summary.FieldPaths, other.FieldPaths...)
	summary.MatchTypes = gatewayAppendUniqueStrings(summary.MatchTypes, other.MatchTypes...)
	summary.Classifiers = gatewayAppendUniqueStrings(summary.Classifiers, other.Classifiers...)
	summary.PolicyIDs = gatewayAppendUniqueStrings(summary.PolicyIDs, other.PolicyIDs...)
}
func (summary *gatewayRedactionAuditSummary) add(target, fieldPath, matchType, classifier string) {
	if summary == nil {
		return
	}
	target = normalizeGatewayRedactionTarget(target)
	if target == "" {
		target = "input"
	}
	matchType = strings.TrimSpace(matchType)
	if matchType == "" {
		return
	}
	summary.TotalMatches++
	switch matchType {
	case "field":
		summary.FieldMatches++
	case "sensitive_key":
		summary.SensitiveKeyMatches++
	case "sensitive_text":
		summary.SensitiveTextMatches++
	case "value_pattern":
		summary.ValuePatternMatches++
	case "secret_classifier":
		summary.SecretClassifierMatches++
	case "structured_secret":
		summary.StructuredSecretMatches++
	}
	summary.Targets = gatewayAppendUniqueStrings(summary.Targets, target)
	summary.MatchTypes = gatewayAppendUniqueStrings(summary.MatchTypes, matchType)
	if fieldPath = gatewayAuditFieldPath(fieldPath); fieldPath != "" {
		summary.FieldPaths = gatewayAppendUniqueStrings(summary.FieldPaths, fieldPath)
	}
	if classifier = strings.TrimSpace(classifier); classifier != "" {
		summary.Classifiers = gatewayAppendUniqueStrings(summary.Classifiers, classifier)
	}
}
func (summary gatewayRedactionAuditSummary) toMap() map[string]any {
	if summary.empty() {
		return nil
	}
	return map[string]any{
		"totalMatches":            summary.TotalMatches,
		"fieldMatches":            summary.FieldMatches,
		"sensitiveKeyMatches":     summary.SensitiveKeyMatches,
		"sensitiveTextMatches":    summary.SensitiveTextMatches,
		"valuePatternMatches":     summary.ValuePatternMatches,
		"secretClassifierMatches": summary.SecretClassifierMatches,
		"structuredSecretMatches": summary.StructuredSecretMatches,
		"targets":                 gatewayLimitedSortedStrings(summary.Targets, 12),
		"fieldPaths":              gatewayLimitedSortedStrings(summary.FieldPaths, 24),
		"matchTypes":              gatewayLimitedSortedStrings(summary.MatchTypes, 12),
		"classifiers":             gatewayLimitedSortedStrings(summary.Classifiers, 24),
		"policyIds":               gatewayLimitedSortedStrings(summary.PolicyIDs, 24),
	}
}
func gatewayRedactionRules(values map[string]any, tool domainaigateway.ToolCapability, target string) []gatewayRedactionRule {
	target = normalizeGatewayRedactionTarget(target)
	base := gatewayBuildRedactionRule(values, gatewayRedactionRule{Target: "input"})
	out := make([]gatewayRedactionRule, 0, 1)
	if gatewayRedactionRuleConfigured(base) && gatewayRedactionRuleTargetMatches(base, target) {
		out = append(out, base)
	}
	for _, rawRule := range gatewayRedactionRuleMaps(values) {
		if !gatewayRedactionRuleAppliesToTool(rawRule, tool) {
			continue
		}
		rule := gatewayBuildRedactionRule(rawRule, base)
		if gatewayRedactionRuleConfigured(rule) && gatewayRedactionRuleTargetMatches(rule, target) {
			out = append(out, rule)
		}
	}
	return out
}
func gatewayBuildRedactionRule(values map[string]any, fallback gatewayRedactionRule) gatewayRedactionRule {
	rule := fallback
	if rule.Replacement == "" {
		rule.Replacement = "[REDACTED]"
	}
	if target := normalizeGatewayRedactionTarget(gatewayFirstString(values, "target", "appliesTo", "direction")); target != "" {
		rule.Target = target
	}
	if mode := normalizeGatewayRedactionMode(gatewayFirstString(values, "mode", "strategy", "redactionMode", "action")); mode != "" {
		rule.Mode = mode
	}
	if fields := gatewayConditionStringList(values, "fields", "field", "paths", "path", "redactFields", "maskFields", "sensitiveFields"); len(fields) > 0 {
		rule.Fields = fields
	}
	if fields := gatewayConditionStringList(values, "allowFields", "allowedFields", "allowlist", "fieldAllowList", "fieldAllowlist"); len(fields) > 0 {
		rule.AllowFields = fields
	}
	if patterns := gatewayConditionStringList(values, "valuePatterns", "valuePattern", "valueRegex", "valueRegexes", "regex", "regexes", "matchValues", "matchPatterns"); len(patterns) > 0 {
		rule.ValuePatterns = patterns
	}
	if secretTypes := gatewayConditionStringList(values, "secretTypes", "secretType", "classifiers", "classifier", "detect", "detectSecretTypes", "secretClassifiers"); len(secretTypes) > 0 {
		rule.SecretTypes = secretTypes
	}
	if gatewayFirstBool(values, "detectSecrets", "classifySecrets", "structuredSecrets") {
		rule.SecretTypes = append(rule.SecretTypes, "default")
	}
	if replacement := gatewayFirstString(values, "replacement", "replacementText", "redactionValue", "maskValue"); replacement != "" {
		rule.Replacement = replacement
	}
	if gatewayFirstBool(values, "preserveFormat", "formatPreserving", "preserveShape") {
		rule.PreserveFormat = true
	}
	if gatewayFirstBool(values, "denySensitiveInput", "blockSensitiveInput", "rejectSensitiveInput") {
		rule.DenySensitive = true
		rule.SanitizeSensitive = false
	}
	if gatewayFirstBool(values, "sanitizeInput", "maskSensitiveInput", "redactInput") {
		rule.SanitizeSensitive = true
	}
	switch rule.Mode {
	case "strict", "deny_sensitive", "block_sensitive", "reject_sensitive", "deny", "block":
		rule.DenySensitive = true
		rule.SanitizeSensitive = false
	case "sanitize", "sanitise", "mask", "redact", "redacted", "sanitize_input", "mask_input", "redact_input":
		rule.SanitizeSensitive = true
	}
	if rule.Mode == "" && (len(rule.Fields) > 0 || len(rule.AllowFields) > 0 || len(rule.ValuePatterns) > 0 || len(rule.SecretTypes) > 0) {
		rule.SanitizeSensitive = true
	}
	return rule
}
func normalizeGatewayRedactionTarget(target string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	target = strings.ReplaceAll(target, "-", "_")
	target = strings.ReplaceAll(target, " ", "_")
	switch target {
	case "", "default":
		return ""
	case "input", "request", "tool_input", "before_invoke", "pre_invoke":
		return "input"
	case "output", "response", "tool_output", "after_invoke", "post_invoke", "result":
		return "output"
	case "both", "input_output", "request_response", "all":
		return "both"
	default:
		return target
	}
}
func gatewayRedactionRuleTargetMatches(rule gatewayRedactionRule, target string) bool {
	target = normalizeGatewayRedactionTarget(target)
	ruleTarget := normalizeGatewayRedactionTarget(rule.Target)
	if ruleTarget == "" {
		ruleTarget = "input"
	}
	if target == "" {
		target = "input"
	}
	return ruleTarget == "both" || ruleTarget == target
}
func gatewayRedactionRuleConfigured(rule gatewayRedactionRule) bool {
	return rule.DenySensitive || rule.SanitizeSensitive
}
func normalizeGatewayRedactionMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	mode = strings.ReplaceAll(mode, "-", "_")
	mode = strings.ReplaceAll(mode, " ", "_")
	return mode
}
func gatewayRedactionRuleMaps(values map[string]any) []map[string]any {
	rawValues := make([]any, 0, 4)
	for _, key := range []string{"rules", "fieldRules", "toolRules", "redactionRules"} {
		if raw, ok := gatewayConditionRaw(values, key); ok {
			rawValues = append(rawValues, raw)
		}
	}
	out := make([]map[string]any, 0)
	for _, raw := range rawValues {
		switch typed := raw.(type) {
		case []map[string]any:
			out = append(out, typed...)
		case []any:
			for _, item := range typed {
				if mapped := mapValue(item); len(mapped) > 0 {
					out = append(out, mapped)
				}
			}
		case map[string]any:
			out = append(out, typed)
		}
	}
	return out
}
func gatewayRedactionRuleAppliesToTool(rule map[string]any, tool domainaigateway.ToolCapability) bool {
	patterns := gatewayConditionStringList(rule, "tool", "tools", "toolName", "toolNames", "toolPattern", "toolPatterns")
	if len(patterns) == 0 {
		return true
	}
	return matchesToolPatternList(patterns, tool.Name)
}
func gatewayRedactionAuditSummaryForValue(value any, rule gatewayRedactionRule, target string) gatewayRedactionAuditSummary {
	var summary gatewayRedactionAuditSummary
	gatewayCollectRedactionAudit(value, rule, target, "", &summary)
	return summary
}
func gatewayCollectRedactionAudit(value any, rule gatewayRedactionRule, target, path string, summary *gatewayRedactionAuditSummary) {
	if summary == nil || gatewayRedactionPathAllowed(path, rule.AllowFields) {
		return
	}
	for _, classifier := range gatewayStructuredSecretClassifiers(value, rule) {
		summary.add(target, path, "structured_secret", classifier)
	}
	switch typed := value.(type) {
	case nil:
		return
	case map[string]any:
		for key, item := range typed {
			nextPath := gatewayJoinFieldPath(path, key)
			if gatewayRedactionPathAllowed(nextPath, rule.AllowFields) {
				continue
			}
			if gatewayRedactionFieldMatches(nextPath, key, rule.Fields) {
				summary.add(target, nextPath, "field", "")
			}
			if gatewaySensitiveKey(key) {
				summary.add(target, nextPath, "sensitive_key", "")
			}
			gatewayCollectRedactionAudit(item, rule, target, nextPath, summary)
		}
	case []any:
		for index, item := range typed {
			gatewayCollectRedactionAudit(item, rule, target, gatewayJoinFieldPath(path, strconv.Itoa(index)), summary)
		}
	case []map[string]any:
		for index, item := range typed {
			gatewayCollectRedactionAudit(item, rule, target, gatewayJoinFieldPath(path, strconv.Itoa(index)), summary)
		}
	case []string:
		for index, item := range typed {
			gatewayCollectRedactionStringAudit(item, rule, target, gatewayJoinFieldPath(path, strconv.Itoa(index)), summary)
		}
	case string:
		gatewayCollectRedactionStringAudit(typed, rule, target, path, summary)
	}
}
func gatewayCollectRedactionStringAudit(value string, rule gatewayRedactionRule, target, path string, summary *gatewayRedactionAuditSummary) {
	if summary == nil || strings.TrimSpace(value) == "" {
		return
	}
	if gatewaySensitiveValuePattern.MatchString(value) {
		summary.add(target, path, "sensitive_text", "")
	}
	for _, pattern := range gatewayCompiledRedactionPatterns(rule.ValuePatterns) {
		if pattern.MatchString(value) {
			summary.add(target, path, "value_pattern", gatewayRegexSummary(pattern))
		}
	}
	for _, classifier := range gatewaySecretClassifierMatches(value, rule.SecretTypes) {
		summary.add(target, path, "secret_classifier", classifier)
	}
}
func applyGatewayRedactionRule(values map[string]any, rule gatewayRedactionRule) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out, ok := applyGatewayRedactionValue(values, rule, "").(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return out
}
func gatewayRedactionSerializableValue(value any) any {
	switch value.(type) {
	case nil, map[string]any, []any, []map[string]any, []string, string:
		return value
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return value
		}
		return out
	}
}
func applyGatewayRedactionValue(value any, rule gatewayRedactionRule, path string) any {
	if gatewayRedactionPathAllowed(path, rule.AllowFields) {
		return cloneGatewayValue(value)
	}
	if len(gatewayStructuredSecretClassifiers(value, rule)) > 0 {
		return gatewayRedactionReplacementForValue(value, rule)
	}
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			nextPath := gatewayJoinFieldPath(path, key)
			if gatewayRedactionPathAllowed(nextPath, rule.AllowFields) {
				out[key] = cloneGatewayValue(item)
				continue
			}
			if gatewayRedactionFieldMatches(nextPath, key, rule.Fields) || gatewaySensitiveKey(key) {
				out[key] = gatewayRedactionReplacementForValue(item, rule)
				continue
			}
			out[key] = applyGatewayRedactionValue(item, rule, nextPath)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = applyGatewayRedactionValue(item, rule, gatewayJoinFieldPath(path, strconv.Itoa(index)))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for index, item := range typed {
			value := applyGatewayRedactionValue(item, rule, gatewayJoinFieldPath(path, strconv.Itoa(index)))
			if mapped, ok := value.(map[string]any); ok {
				out[index] = mapped
			} else {
				out[index] = map[string]any{}
			}
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for index, item := range typed {
			out[index] = gatewayRedactSensitiveTextWithRule(item, rule)
		}
		return out
	case string:
		return gatewayRedactSensitiveTextWithRule(typed, rule)
	default:
		return typed
	}
}
func gatewayRedactionReplacementForValue(value any, rule gatewayRedactionRule) any {
	switch typed := value.(type) {
	case map[string]any, []any, []map[string]any:
		return applyGatewayRedactionValue(value, gatewayRedactionRule{
			Fields:            []string{"*"},
			Replacement:       gatewayRedactionReplacement(rule),
			PreserveFormat:    rule.PreserveFormat,
			SanitizeSensitive: true,
		}, "")
	case []string:
		out := make([]string, len(typed))
		for index, item := range typed {
			out[index] = gatewayMaskString(item, rule)
		}
		return out
	case string:
		return gatewayMaskString(typed, rule)
	default:
		return gatewayRedactionReplacement(rule)
	}
}
func gatewayStructuredSecretClassifiers(value any, rule gatewayRedactionRule) []string {
	if len(rule.SecretTypes) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	switch typed := value.(type) {
	case map[string]any:
		if gatewaySecretTypeEnabled(rule.SecretTypes, "kubernetes_secret", "k8s_secret", "kubernetes", "k8s") && gatewayKubernetesSecretMap(typed) {
			out = gatewayAppendUniqueStrings(out, "kubernetes_secret")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "kubeconfig", "kubernetes_config", "k8sconfig", "k8s_config") && gatewayKubeconfigMap(typed) {
			out = gatewayAppendUniqueStrings(out, "kubeconfig")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "docker_config", "dockerconfig", "docker_auth", "docker") && gatewayDockerConfigMap(typed) {
			out = gatewayAppendUniqueStrings(out, "docker_config")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "gcp_service_account", "google_service_account", "service_account_json", "google") && gatewayGCPServiceAccountMap(typed) {
			out = gatewayAppendUniqueStrings(out, "gcp_service_account")
		}
		if gatewaySecretTypeEnabled(rule.SecretTypes, "aws", "aws_credentials", "awscredential") && gatewayAWSCredentialsMap(typed) {
			out = gatewayAppendUniqueStrings(out, "aws")
		}
	case []any:
		for _, item := range typed {
			out = gatewayAppendUniqueStrings(out, gatewayStructuredSecretClassifiers(item, rule)...)
		}
	case []map[string]any:
		for _, item := range typed {
			out = gatewayAppendUniqueStrings(out, gatewayStructuredSecretClassifiers(item, rule)...)
		}
	}
	return out
}
func gatewayKubernetesSecretMap(values map[string]any) bool {
	if gatewayStringSliceContainsAny(gatewayStringList(values["kind"]), "secret", "kubernetes_secret", "kubernetes") {
		return true
	}
	if gatewayStringSliceContainsAny(gatewayStringList(values["resourceKind"]), "Secret", "secret") {
		return true
	}
	if _, ok := values["data"]; ok && gatewayStringSliceContainsAny(gatewayStringList(values["apiVersion"]), "v1") && gatewayStringSliceContainsAny(gatewayStringList(values["kind"]), "Secret") {
		return true
	}
	if _, ok := values["stringData"]; ok && gatewayStringSliceContainsAny(gatewayStringList(values["kind"]), "Secret") {
		return true
	}
	return false
}
func gatewayKubeconfigMap(values map[string]any) bool {
	_, hasClusters := gatewayConditionRaw(values, "clusters")
	_, hasContexts := gatewayConditionRaw(values, "contexts")
	_, hasUsers := gatewayConditionRaw(values, "users")
	_, hasCurrentContext := gatewayConditionRaw(values, "current-context")
	if !hasCurrentContext {
		_, hasCurrentContext = gatewayConditionRaw(values, "currentContext")
	}
	return hasClusters && hasContexts && hasUsers && hasCurrentContext
}
func gatewayDockerConfigMap(values map[string]any) bool {
	if raw, ok := gatewayConditionRaw(values, "auths"); ok && len(mapValue(raw)) > 0 {
		return true
	}
	if raw, ok := gatewayConditionRaw(values, "credHelpers"); ok && len(mapValue(raw)) > 0 {
		return true
	}
	if text := gatewayFirstString(values, "credsStore", "credStore"); text != "" {
		return true
	}
	return false
}
func gatewayGCPServiceAccountMap(values map[string]any) bool {
	if !gatewayStringSliceContainsAny(gatewayStringList(values["type"]), "service_account") {
		return false
	}
	_, hasPrivateKey := gatewayConditionRaw(values, "private_key")
	_, hasClientEmail := gatewayConditionRaw(values, "client_email")
	return hasPrivateKey && hasClientEmail
}
func gatewayAWSCredentialsMap(values map[string]any) bool {
	_, hasAccessKey := gatewayConditionRaw(values, "aws_access_key_id")
	if !hasAccessKey {
		_, hasAccessKey = gatewayConditionRaw(values, "accessKeyId")
	}
	_, hasSecretKey := gatewayConditionRaw(values, "aws_secret_access_key")
	if !hasSecretKey {
		_, hasSecretKey = gatewayConditionRaw(values, "secretAccessKey")
	}
	return hasAccessKey && hasSecretKey
}
func gatewaySecretTypeEnabled(secretTypes []string, aliases ...string) bool {
	aliasSet := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		aliasSet[normalizeGatewayConditionKey(alias)] = struct{}{}
	}
	for _, secretType := range secretTypes {
		switch normalizeGatewayConditionKey(secretType) {
		case "default", "all", "builtin", "builtins", "secret", "secrets":
			return true
		default:
			if _, ok := aliasSet[normalizeGatewayConditionKey(secretType)]; ok {
				return true
			}
		}
	}
	return false
}
func gatewayCompiledRedactionPatterns(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		out = append(out, compiled)
	}
	return out
}
func gatewaySecretClassifierPatterns(secretTypes []string) []*regexp.Regexp {
	patterns := gatewaySecretClassifierPatternSpecs(secretTypes)
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, pattern.Pattern)
	}
	return out
}

type gatewaySecretClassifierPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

func gatewaySecretClassifierPatternSpecs(secretTypes []string) []gatewaySecretClassifierPattern {
	out := make([]gatewaySecretClassifierPattern, 0, len(secretTypes)+4)
	seen := map[string]struct{}{}
	for _, secretType := range secretTypes {
		normalized := normalizeGatewayConditionKey(secretType)
		for _, definition := range gatewaySecretClassifierDefinitionsForType(normalized) {
			if _, ok := seen[definition.name]; ok {
				continue
			}
			seen[definition.name] = struct{}{}
			out = append(out, gatewaySecretClassifierPattern{
				Name:    definition.name,
				Pattern: regexp.MustCompile(definition.pattern),
			})
		}
	}
	return out
}
func gatewaySecretClassifierMatches(value string, secretTypes []string) []string {
	out := make([]string, 0)
	for _, classifier := range gatewaySecretClassifierPatternSpecs(secretTypes) {
		if classifier.Pattern.MatchString(value) {
			out = gatewayAppendUniqueStrings(out, classifier.Name)
		}
	}
	return out
}
func gatewayRegexSummary(pattern *regexp.Regexp) string {
	if pattern == nil {
		return ""
	}
	text := pattern.String()
	if len(text) <= 80 {
		return text
	}
	return text[:80]
}
func gatewayReplaceRegexMatches(value string, pattern *regexp.Regexp, rule gatewayRedactionRule) string {
	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		return gatewayMaskString(match, rule)
	})
}
func gatewayStringSliceContainsAny(values []string, needles ...string) bool {
	for _, value := range values {
		for _, needle := range needles {
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
				return true
			}
		}
	}
	return false
}
func gatewayAppendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	out := make([]string, 0, len(values)+len(additions))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	for _, value := range additions {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	return out
}
func gatewayLimitedSortedStrings(values []string, limit int) []string {
	values = gatewayAppendUniqueStrings(nil, values...)
	sort.Strings(values)
	if limit > 0 && len(values) > limit {
		return values[:limit]
	}
	return values
}
func gatewayAuditFieldPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "$"
	}
	parts := strings.Split(path, ".")
	for index, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			parts[index] = "*"
		}
	}
	return strings.Join(parts, ".")
}
func gatewayRedactSensitiveTextWithRule(value string, rule gatewayRedactionRule) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	out := gatewaySensitiveValuePattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := gatewaySensitiveValuePattern.FindStringSubmatch(match)
		if len(parts) < 4 {
			return gatewayRedactionReplacement(rule)
		}
		replacement := gatewayRedactionReplacement(rule)
		if rule.PreserveFormat {
			replacement = gatewayMaskString(parts[3], rule)
		}
		return parts[1] + parts[2] + replacement
	})
	for _, pattern := range gatewayCompiledRedactionPatterns(rule.ValuePatterns) {
		out = gatewayReplaceRegexMatches(out, pattern, rule)
	}
	for _, pattern := range gatewaySecretClassifierPatterns(rule.SecretTypes) {
		out = gatewayReplaceRegexMatches(out, pattern, rule)
	}
	return out
}
func gatewayMaskString(value string, rule gatewayRedactionRule) string {
	replacement := gatewayRedactionReplacement(rule)
	if !rule.PreserveFormat {
		return replacement
	}
	if value == "" {
		return replacement
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-4:])
}
func gatewayRedactionReplacement(rule gatewayRedactionRule) string {
	if rule.Replacement != "" {
		return rule.Replacement
	}
	return "[REDACTED]"
}
func gatewayJoinFieldPath(parent, child string) string {
	child = strings.TrimSpace(child)
	if child == "" {
		return parent
	}
	if parent == "" {
		return child
	}
	return parent + "." + child
}
func gatewayRedactionPathAllowed(path string, patterns []string) bool {
	if strings.TrimSpace(path) == "" || len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if gatewayFieldPathMatches(pattern, path, "") {
			return true
		}
	}
	return false
}
func gatewayRedactionFieldMatches(path, key string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if gatewayFieldPathMatches(pattern, path, key) {
			return true
		}
	}
	return false
}
func gatewayFieldPathMatches(pattern, path, key string) bool {
	pattern = normalizeGatewayFieldPath(pattern)
	path = normalizeGatewayFieldPath(path)
	key = normalizeGatewayFieldPath(key)
	if pattern == "" || path == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if pattern == path || pattern == key {
		return true
	}
	if gatewayFieldPathSegmentPatternMatches(pattern, path) || (key != "" && gatewayFieldPathSegmentPatternMatches(pattern, key)) {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*.")
		return path == suffix || strings.HasSuffix(path, "."+suffix)
	}
	return false
}
func gatewayFieldPathSegmentPatternMatches(pattern, path string) bool {
	if pattern == "" || path == "" {
		return false
	}
	patternParts := strings.Split(pattern, ".")
	pathParts := strings.Split(path, ".")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for index, patternPart := range patternParts {
		if patternPart == "*" {
			continue
		}
		if patternPart != pathParts[index] {
			return false
		}
	}
	return true
}
func normalizeGatewayFieldPath(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "[*]", ".*")
	value = strings.ReplaceAll(value, "[]", "")
	value = strings.Trim(value, ".")
	for strings.Contains(value, "..") {
		value = strings.ReplaceAll(value, "..", ".")
	}
	return value
}
func gatewayConditionValues(conditions map[string]any, aliases ...string) map[string]any {
	out := make(map[string]any, len(conditions)+4)
	for key, value := range conditions {
		out[key] = value
	}
	for _, alias := range aliases {
		raw, ok := gatewayConditionRaw(conditions, alias)
		if !ok {
			continue
		}
		for key, value := range mapValue(raw) {
			out[key] = value
		}
	}
	return out
}
func gatewayFirstPositiveInt(values map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		value, ok := gatewayPositiveInt(raw)
		if ok {
			return value, true
		}
	}
	return 0, false
}
func gatewayFirstPositiveFloat(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		value, ok := gatewayPositiveFloat(raw)
		if ok {
			return value, true
		}
	}
	return 0, false
}
func gatewayPositiveFloatSum(values map[string]any, keys ...string) float64 {
	total := 0.0
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		value, ok := gatewayPositiveFloat(raw)
		if ok {
			total += value
		}
	}
	return total
}
func gatewayFirstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok || raw == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(raw))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}
func gatewayFirstBool(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		if boolFromAny(raw) {
			return true
		}
	}
	return false
}
func gatewayConditionStringList(values map[string]any, keys ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, key := range keys {
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		for _, value := range gatewayStringList(raw) {
			normalized := strings.TrimSpace(value)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}
func gatewayStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '\n' || r == ';'
		})
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if text := strings.TrimSpace(part); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
func gatewayConditionRaw(values map[string]any, key string) (any, bool) {
	if values == nil {
		return nil, false
	}
	if value, ok := values[key]; ok {
		return value, true
	}
	normalized := normalizeGatewayConditionKey(key)
	for candidate, value := range values {
		if normalizeGatewayConditionKey(candidate) == normalized {
			return value, true
		}
	}
	return nil, false
}
func normalizeGatewayConditionKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(key)))
}
func gatewayPositiveInt(value any) (int, bool) {
	return gatewayInt(value, func(value int) bool { return value > 0 })
}
func gatewayNonNegativeInt(value any) (int, bool) {
	return gatewayInt(value, func(value int) bool { return value >= 0 })
}
func gatewayInt(value any, valid func(int) bool) (int, bool) {
	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int32:
		parsed = int(typed)
	case int64:
		parsed = int(typed)
	case float32:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case json.Number:
		integer, err := typed.Int64()
		if err != nil {
			asFloat, floatErr := strconv.ParseFloat(typed.String(), 64)
			if floatErr != nil {
				return 0, false
			}
			parsed = int(asFloat)
			break
		}
		parsed = int(integer)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		asFloat, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		parsed = int(asFloat)
	default:
		return 0, false
	}
	return parsed, valid(parsed)
}
func gatewayPositiveFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		value := float64(typed)
		return value, value > 0
	case int32:
		value := float64(typed)
		return value, value > 0
	case int64:
		value := float64(typed)
		return value, value > 0
	case float32:
		value := float64(typed)
		return value, value > 0
	case float64:
		return typed, typed > 0
	case json.Number:
		parsed, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return 0, false
		}
		return parsed, parsed > 0
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return parsed, parsed > 0
	default:
		return 0, false
	}
}
func gatewayConditionWindow(values map[string]any, fallback time.Duration, fallbackLabel string) (time.Duration, string) {
	if seconds, ok := gatewayFirstPositiveInt(values, "windowSeconds", "windowSecond", "seconds"); ok {
		duration := time.Duration(seconds) * time.Second
		return duration, formatGatewayWindowLabel(duration)
	}
	if minutes, ok := gatewayFirstPositiveInt(values, "windowMinutes", "windowMinute", "minutes"); ok {
		duration := time.Duration(minutes) * time.Minute
		return duration, formatGatewayWindowLabel(duration)
	}
	if hours, ok := gatewayFirstPositiveInt(values, "windowHours", "windowHour", "hours"); ok {
		duration := time.Duration(hours) * time.Hour
		return duration, formatGatewayWindowLabel(duration)
	}
	text := gatewayFirstString(values, "window", "windowDuration", "duration")
	if text != "" {
		if duration, err := time.ParseDuration(text); err == nil && duration > 0 {
			return duration, formatGatewayWindowLabel(duration)
		}
	}
	return fallback, fallbackLabel
}
func formatGatewayWindowLabel(duration time.Duration) string {
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
	return duration.String()
}
func gatewayLimitScope(values map[string]any, fallback string) string {
	scope := normalizeGatewayLimitScope(gatewayFirstString(values, "scope", "limitScope", "counterScope"))
	if scope == "" {
		return normalizeGatewayLimitScope(fallback)
	}
	return scope
}
func normalizeGatewayLimitScope(scope string) string {
	normalized := strings.ToLower(strings.TrimSpace(scope))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "", "default":
		return ""
	case "global", "shared":
		return "global"
	case "client", "ai_client", "per_client":
		return "client"
	case "client_tool", "ai_client_tool", "per_client_tool":
		return "client_tool"
	case "actor", "subject", "user", "service_account", "per_actor", "per_user", "per_subject":
		return "actor"
	case "actor_tool", "subject_tool", "user_tool", "per_actor_tool", "per_user_tool", "per_subject_tool":
		return "actor_tool"
	case "actor_client", "subject_client", "user_client", "per_actor_client", "per_user_client", "per_subject_client":
		return "actor_client"
	default:
		return "actor_client_tool"
	}
}
