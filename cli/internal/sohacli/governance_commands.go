package sohacli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func runAudit(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("audit requires a subcommand: list")
	}
	switch args[0] {
	case "list":
		return runAuditList(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown audit command %q", args[0])
	}
}

func runAuditList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("audit list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	actor := fs.String("actor", "", "actor id")
	actorType := fs.String("actor-type", "", "actor type")
	aiClientID := fs.String("ai-client-id", "", "AI client id")
	skillID := fs.String("skill-id", "", "skill id")
	toolName := fs.String("tool-name", "", "tool name")
	riskLevel := fs.String("risk-level", "", "risk level")
	result := fs.String("result", "", "result")
	action := fs.String("action", "", "action")
	approvalRequestID := fs.String("approval-request-id", "", "approval request id")
	from := fs.String("from", "", "RFC3339 start time")
	to := fs.String("to", "", "RFC3339 end time")
	limit := fs.Int("limit", 100, "result limit")
	format := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "json", "yaml")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	query := url.Values{}
	setQuery(query, "actor", *actor)
	setQuery(query, "actorType", *actorType)
	setQuery(query, "aiClientId", *aiClientID)
	setQuery(query, "skillId", *skillID)
	setQuery(query, "toolName", *toolName)
	setQuery(query, "riskLevel", *riskLevel)
	setQuery(query, "result", *result)
	setQuery(query, "action", *action)
	setQuery(query, "approvalRequestId", *approvalRequestID)
	setQuery(query, "from", *from)
	setQuery(query, "to", *to)
	if *limit > 0 {
		query.Set("limit", fmt.Sprint(*limit))
	}
	items, err := gatewayClient(rt, profile).ListAuditLogs(ctx, query, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(items))
}

func runApproval(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("approval requires a subcommand: list, timeline, approve, reject, or cancel")
	}
	switch args[0] {
	case "list":
		return runApprovalList(ctx, args[1:], rt)
	case "timeline":
		return runApprovalTimeline(ctx, args[1:], rt)
	case "approve", "reject", "cancel":
		return runApprovalDecision(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown approval command %q", args[0])
	}
}

func runApprovalList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("approval list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	id := fs.String("id", "", "approval request id")
	status := fs.String("status", "", "approval request status")
	actor := fs.String("actor", "", "actor id")
	actorType := fs.String("actor-type", "", "actor type")
	aiClientID := fs.String("ai-client-id", "", "AI client id")
	skillID := fs.String("skill-id", "", "skill id")
	toolName := fs.String("tool-name", "", "tool name")
	riskLevel := fs.String("risk-level", "", "risk level")
	strategy := fs.String("strategy", "", "approval strategy")
	from := fs.String("from", "", "RFC3339 start time")
	to := fs.String("to", "", "RFC3339 end time")
	limit := fs.Int("limit", 100, "result limit")
	format := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "json", "yaml")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	query := url.Values{}
	setQuery(query, "approvalRequestId", *id)
	setQuery(query, "status", *status)
	setQuery(query, "actor", *actor)
	setQuery(query, "actorType", *actorType)
	setQuery(query, "aiClientId", *aiClientID)
	setQuery(query, "skillId", *skillID)
	setQuery(query, "toolName", *toolName)
	setQuery(query, "riskLevel", *riskLevel)
	setQuery(query, "strategy", *strategy)
	setQuery(query, "from", *from)
	setQuery(query, "to", *to)
	if *limit > 0 {
		query.Set("limit", fmt.Sprint(*limit))
	}
	items, err := gatewayClient(rt, profile).ListApprovalRequests(ctx, query, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(items))
}

func runApprovalTimeline(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("approval timeline", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "json", "output format: json or yaml")
	requestID := ""
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		requestID = strings.TrimSpace(args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if requestID == "" && fs.NArg() > 0 {
		requestID = strings.TrimSpace(fs.Arg(0))
	}
	if requestID == "" {
		return fmt.Errorf("approval timeline requires an approval request id")
	}
	formatValue, err := normalizeOutputFormat(*format, "json", "yaml")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).GetApprovalTimeline(ctx, requestID, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(item))
}

func runApprovalDecision(ctx context.Context, action string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("approval "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	comment := fs.String("comment", "", "decision comment")
	requestID := ""
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		requestID = strings.TrimSpace(args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if requestID == "" && fs.NArg() > 0 {
		requestID = strings.TrimSpace(fs.Arg(0))
	}
	if requestID == "" {
		return fmt.Errorf("approval %s requires an approval request id", action)
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	result, err := gatewayClient(rt, profile).DecideApprovalRequest(ctx, requestID, action, *comment, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(result))
}

func runGovernance(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("governance requires a subcommand: status")
	}
	switch args[0] {
	case "status":
		return runGovernanceStatus(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown governance command %q", args[0])
	}
}

func runGovernanceStatus(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("governance status", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	windowHours := fs.Int("window-hours", 24, "audit window in hours")
	format := fs.String("output", "table", "output format: table, json, or yaml")
	jsonOut := fs.Bool("json", false, "print full JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *windowHours < 1 || *windowHours > 168 {
		return fmt.Errorf("window-hours must be between 1 and 168")
	}
	if *jsonOut {
		*format = "json"
	}
	formatValue, err := normalizeOutputFormat(*format, "table", "json", "yaml")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	status, err := gatewayClient(rt, profile).GovernanceStatus(ctx, *windowHours, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	switch formatValue {
	case "", "table":
	case "json":
		return writePrettyJSON(rt.Out, sanitizeCLIValue(status))
	case "yaml":
		return writeYAML(rt.Out, sanitizeCLIValue(status))
	}
	out := newCheckedWriter(rt.Out)
	out.Printf("health: %s\t%s\n", status.Health.Status, redactSensitiveText(status.Health.Message))
	for _, check := range status.Health.Checks {
		out.Printf("healthCheck: %s\t%s\tcount=%d\t%s\n", redactSensitiveText(check.Status), redactSensitiveText(check.Name), check.Count, redactSensitiveText(check.Message))
	}
	out.Printf("windowHours: %d\n", status.WindowHours)
	out.Printf("calls: total=%d success=%d deny=%d failure=%d pendingApproval=%d dryRun=%d\n",
		status.Metrics.TotalCalls,
		status.Metrics.SuccessCount,
		status.Metrics.DenyCount,
		status.Metrics.FailureCount,
		status.Metrics.PendingApprovalCount,
		status.Metrics.DryRunCount,
	)
	out.Printf("tokens: personalActive=%d serviceActive=%d expiringSoon=%d expiredActive=%d stale=%d neverUsed=%d lastUsed=%s\n",
		status.Tokens.PersonalAccessTokens.Active,
		status.Tokens.ServiceAccountTokens.Active,
		len(status.Tokens.ExpiringSoon),
		len(status.Tokens.ExpiredActive),
		len(status.Tokens.Stale),
		len(status.Tokens.NeverUsed),
		status.Tokens.LastUsedTrackingState,
	)
	out.Printf("clients: total=%d active=%d pendingApproval=%d registrationApproval=%s\n",
		status.Clients.Total,
		status.Clients.Active,
		status.Clients.PendingApproval,
		status.Clients.RegistrationApproval,
	)
	nextDue := ""
	if status.Approvals.NextDueAt != nil {
		nextDue = status.Approvals.NextDueAt.Format(time.RFC3339)
	}
	out.Printf("approvals: pending=%d dueSoon=%d stale=%d overdue=%d oldestPendingHours=%d nextDue=%s\n",
		status.Approvals.Pending,
		status.Approvals.DueSoon,
		status.Approvals.StalePending,
		status.Approvals.Overdue,
		status.Approvals.OldestPendingHours,
		nextDue,
	)
	out.Printf("policyCoverage: access=%d activeAccess=%d grants=%d activeGrants=%d skills=%d activeSkills=%d budget=%s rateLimit=%s redaction=%s resourceScopes=%s scopedAccess=%d scopedGrants=%d\n",
		status.PolicyCoverage.AccessPolicies,
		status.PolicyCoverage.ActiveAccessPolicies,
		status.PolicyCoverage.ToolGrants,
		status.PolicyCoverage.ActiveToolGrants,
		status.PolicyCoverage.SkillBindings,
		status.PolicyCoverage.ActiveSkillBindings,
		status.PolicyCoverage.BudgetState,
		status.PolicyCoverage.RateLimitState,
		status.PolicyCoverage.RedactionPolicyState,
		status.PolicyCoverage.ResourceScopeState,
		status.PolicyCoverage.ResourceScopedAccessPolicies,
		status.PolicyCoverage.ResourceScopedToolGrants,
	)
	out.Printf("redaction: matches=%d audits=%d inputAudits=%d outputAudits=%d field=%d sensitiveKey=%d sensitiveText=%d valuePattern=%d classifier=%d structured=%d\n",
		status.Redaction.TotalMatches,
		status.Redaction.AuditsWithRedaction,
		status.Redaction.InputAudits,
		status.Redaction.OutputAudits,
		status.Redaction.FieldMatches,
		status.Redaction.SensitiveKeyMatches,
		status.Redaction.SensitiveTextMatches,
		status.Redaction.ValuePatternMatches,
		status.Redaction.SecretClassifierMatches,
		status.Redaction.StructuredSecretMatches,
	)
	if summary := governanceMetricCountsSummary(status.Redaction.TopTargets); summary != "" {
		out.Printf("redactionTargets: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopMatchTypes); summary != "" {
		out.Printf("redactionMatchTypes: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopClassifiers); summary != "" {
		out.Printf("redactionClassifiers: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopFieldPaths); summary != "" {
		out.Printf("redactionFieldPaths: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopPolicies); summary != "" {
		out.Printf("redactionPolicies: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopTools); summary != "" {
		out.Printf("redactionTools: %s\n", summary)
	}
	for _, finding := range status.Anomalies {
		out.Printf("finding: %s\t%s\t%d%s\t%s\n", finding.Severity, finding.Type, finding.Count, governanceFindingDetailSuffix(finding), redactSensitiveText(finding.Summary))
	}
	for _, recommendation := range status.Recommendations {
		out.Printf("recommendation: %s\n", redactSensitiveText(recommendation))
	}
	for _, action := range status.RecommendationActions {
		out.Printf("recommendationAction: %s\t%s\taction=%s%s\t%s\n", action.Severity, action.Type, action.Action, governanceRecommendationActionSuffix(action), redactSensitiveText(action.Summary))
	}
	return out.Err()
}

func governanceMetricCountsSummary(items []GovernanceMetricCount) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(redactSensitiveText(item.Key))
		if key == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", key, item.Count))
	}
	return strings.Join(parts, ",")
}

func governanceFindingDetailSuffix(finding GovernanceFinding) string {
	parts := make([]string, 0)
	if finding.ActorType != "" || finding.ActorID != "" {
		parts = append(parts, "actor="+redactSensitiveText(strings.Trim(strings.TrimSpace(finding.ActorType)+":"+strings.TrimSpace(finding.ActorID), ":")))
	}
	if finding.SubjectType != "" || finding.SubjectID != "" {
		parts = append(parts, "subject="+redactSensitiveText(strings.Trim(strings.TrimSpace(finding.SubjectType)+":"+strings.TrimSpace(finding.SubjectID), ":")))
	}
	if finding.AIClientID != "" {
		parts = append(parts, "client="+redactSensitiveText(finding.AIClientID))
	}
	if finding.PolicyID != "" {
		parts = append(parts, "policy="+redactSensitiveText(finding.PolicyID))
	}
	if finding.ApprovalRequestID != "" {
		parts = append(parts, "approval="+redactSensitiveText(finding.ApprovalRequestID))
	}
	if finding.GrantID != "" {
		parts = append(parts, "grant="+redactSensitiveText(finding.GrantID))
	}
	if finding.ToolName != "" {
		parts = append(parts, "tool="+redactSensitiveText(finding.ToolName))
	}
	if finding.RiskLevel != "" {
		parts = append(parts, "risk="+redactSensitiveText(finding.RiskLevel))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\t" + strings.Join(parts, " ")
}

func governanceRecommendationActionSuffix(action GovernanceRecommendationAction) string {
	parts := make([]string, 0)
	if strings.TrimSpace(action.TargetKind) != "" {
		parts = append(parts, "target="+redactSensitiveText(strings.TrimSpace(action.TargetKind)))
	}
	if strings.TrimSpace(action.TargetID) != "" {
		parts = append(parts, "id="+redactSensitiveText(strings.TrimSpace(action.TargetID)))
	}
	if action.Count > 0 {
		parts = append(parts, fmt.Sprintf("count=%d", action.Count))
	}
	if len(action.Refs) > 0 {
		refs := make([]string, 0, len(action.Refs))
		for _, ref := range action.Refs {
			refs = append(refs, redactSensitiveText(ref))
		}
		parts = append(parts, "refs="+strings.Join(refs, ","))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\t" + strings.Join(parts, "\t")
}
