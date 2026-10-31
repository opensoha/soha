package sohacli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func runAI(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("ai requires a subcommand: chat, assistant, runs, task, inspection, evaluation, or memory")
	}
	switch args[0] {
	case "chat":
		return runAIChat(ctx, args[1:], rt)
	case "assistant":
		return runAIAssistant(ctx, args[1:], rt)
	case "runs":
		return runAIAgentRuns(ctx, args[1:], rt)
	case "inspection":
		return runInspection(ctx, args[1:], rt)
	case "task":
		return runCapabilityTask(ctx, args[1:], rt)
	case "evaluation":
		return runAIEvaluation(ctx, args[1:], rt)
	case "memory":
		return runAIMemory(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown ai command %q", args[0])
	}
}

func runAIEvaluation(ctx context.Context, args []string, rt Runtime) error {
	if len(args) > 0 && (args[0] == "datasets" || args[0] == "runs") {
		return runEvaluationCatalog(ctx, args, rt)
	}
	if len(args) == 0 {
		return fmt.Errorf("ai evaluation requires a subcommand: run, replay, or gate")
	}
	switch args[0] {
	case "run":
		return runAIEvaluationRun(ctx, args[1:], rt)
	case "replay":
		return runAIEvaluationReplay(ctx, args[1:], rt)
	case "gate":
		return runAIEvaluationGate(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown ai evaluation command %q", args[0])
	}
}

func runAIEvaluationRun(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("ai evaluation run", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	executorProfileID := fs.String("executor-profile-id", "", "optional executor profile id")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 || strings.TrimSpace(fs.Args()[0]) == "" {
		return fmt.Errorf("ai evaluation run requires one evaluation run id")
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).ExecuteEvaluationRun(ctx, fs.Args()[0], EvaluationExecuteInput{ExecutorProfileID: strings.TrimSpace(*executorProfileID)}, gatewayHeaders(profile, "", "", "evaluation-release-gate-reviewer", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "evaluation-run-executing", item)
}

func runAIEvaluationReplay(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("ai evaluation replay", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	replayID := fs.String("id", "", "replay plan id")
	baselineRunID := fs.String("baseline-run-id", "", "baseline evaluation run id")
	candidateRunID := fs.String("candidate-run-id", "", "candidate evaluation run id")
	executorProfileID := fs.String("executor-profile-id", "", "executor profile id")
	legacyTraceRefs := fs.String("source-trace-refs", "", "deprecated alias for one baseline run id")
	legacyCandidateRefs := fs.String("candidate-refs-json", "", "deprecated candidate run reference JSON")
	legacyReadOnly := fs.Bool("read-only", true, "deprecated; replay is always read-only")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	baseline, candidate, err := resolveReplayRunIDs(*baselineRunID, *candidateRunID, *legacyTraceRefs, *legacyCandidateRefs)
	if err != nil {
		return err
	}
	profileID := strings.TrimSpace(*executorProfileID)
	if baseline == "" || candidate == "" || profileID == "" {
		return fmt.Errorf("--baseline-run-id, --candidate-run-id, and --executor-profile-id are required")
	}
	if !*legacyReadOnly {
		return fmt.Errorf("evaluation replay must remain read-only")
	}
	id := strings.TrimSpace(*replayID)
	if id == "" {
		id = deterministicReplayID(baseline, candidate, profileID)
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).CreateEvaluationReplay(ctx, EvaluationReplayInput{
		ID: id, BaselineRunID: baseline, CandidateRunID: candidate, ExecutorProfileID: profileID,
	}, gatewayHeaders(profile, "", "", "evaluation-release-gate-reviewer", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "evaluation-replay-created", item)
}

func runAIEvaluationGate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("ai evaluation gate", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	policyID := fs.String("policy-id", "", "gate policy id")
	baselineRunID := fs.String("baseline-run-id", "", "baseline evaluation run id")
	candidateRunID := fs.String("candidate-run-id", "", "candidate evaluation run id")
	legacyRunID := fs.String("run-id", "", "deprecated alias for baseline run id")
	legacyCandidateRef := fs.String("candidate-ref", "", "deprecated alias for candidate run id")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	baseline, err := resolveDeprecatedAlias(*baselineRunID, *legacyRunID, "--baseline-run-id", "--run-id")
	if err != nil {
		return err
	}
	candidate, err := resolveDeprecatedAlias(*candidateRunID, *legacyCandidateRef, "--candidate-run-id", "--candidate-ref")
	if err != nil {
		return err
	}
	if strings.TrimSpace(*policyID) == "" || baseline == "" || candidate == "" {
		return fmt.Errorf("--policy-id, --baseline-run-id, and --candidate-run-id are required")
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).EvaluateReleaseGate(ctx, EvaluationGateInput{
		PolicyID: strings.TrimSpace(*policyID), BaselineRunID: baseline, CandidateRunID: candidate,
	}, gatewayHeaders(profile, "", "", "evaluation-release-gate-reviewer", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "evaluation-gate-evaluated", item)
}

func resolveReplayRunIDs(baseline, candidate, legacyTraces, legacyCandidates string) (string, string, error) {
	legacyBaseline := ""
	if refs := splitCommaValues(legacyTraces); len(refs) > 1 {
		return "", "", fmt.Errorf("--source-trace-refs compatibility alias accepts exactly one baseline run id")
	} else if len(refs) == 1 {
		legacyBaseline = refs[0]
	}
	legacyCandidate := ""
	if strings.TrimSpace(legacyCandidates) != "" {
		var refs map[string]string
		if err := json.Unmarshal([]byte(legacyCandidates), &refs); err != nil {
			return "", "", fmt.Errorf("--candidate-refs-json must be a JSON object")
		}
		legacyCandidate = firstNonEmptyString(refs["candidateRunId"], refs["evaluationRun"])
		if legacyCandidate == "" {
			return "", "", fmt.Errorf("--candidate-refs-json compatibility alias requires candidateRunId or evaluationRun")
		}
	}
	resolvedBaseline, err := resolveDeprecatedAlias(baseline, legacyBaseline, "--baseline-run-id", "--source-trace-refs")
	if err != nil {
		return "", "", err
	}
	resolvedCandidate, err := resolveDeprecatedAlias(candidate, legacyCandidate, "--candidate-run-id", "--candidate-refs-json")
	if err != nil {
		return "", "", err
	}
	return resolvedBaseline, resolvedCandidate, nil
}

func resolveDeprecatedAlias(value, alias, valueName, aliasName string) (string, error) {
	value = strings.TrimSpace(value)
	alias = strings.TrimSpace(alias)
	if value != "" && alias != "" && value != alias {
		return "", fmt.Errorf("%s conflicts with deprecated %s", valueName, aliasName)
	}
	return firstNonEmptyString(value, alias), nil
}

func deterministicReplayID(baselineRunID, candidateRunID, executorProfileID string) string {
	digest := sha256.Sum256([]byte(baselineRunID + "\x00" + candidateRunID + "\x00" + executorProfileID))
	return fmt.Sprintf("replay-%x", digest[:8])
}

func runAIMemory(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("ai memory requires a subcommand: inspect or delete")
	}
	switch args[0] {
	case "inspect":
		return runAIMemoryInspect(ctx, args[1:], rt)
	case "delete":
		return runAIMemoryDelete(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown ai memory command %q", args[0])
	}
}

func runAIMemoryInspect(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("ai memory inspect", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	recordID := fs.String("id", "", "optional memory record id")
	ownerType := fs.String("owner-type", "", "memory owner type")
	ownerID := fs.String("owner-id", "", "memory owner id")
	legacySubjectID := fs.String("subject-id", "", "deprecated alias for owner id")
	legacyKind := fs.String("kind", "", "deprecated alias for owner type")
	limit := fs.Int("limit", 50, "maximum records (1-200)")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit < 1 || *limit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200")
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	resolvedOwnerID, err := resolveDeprecatedAlias(*ownerID, *legacySubjectID, "--owner-id", "--subject-id")
	if err != nil {
		return err
	}
	resolvedOwnerType, err := resolveDeprecatedAlias(*ownerType, *legacyKind, "--owner-type", "--kind")
	if err != nil {
		return err
	}
	if resolvedOwnerType == "" {
		resolvedOwnerType = "user"
	}
	query := url.Values{}
	setQueryValue(query, "ownerType", resolvedOwnerType)
	setQueryValue(query, "ownerId", resolvedOwnerID)
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).InspectMemory(ctx, query, gatewayHeaders(profile, "", "", "memory-privacy-curator", "soha-cli"))
	if err != nil {
		return err
	}
	if id := strings.TrimSpace(*recordID); id != "" {
		filtered := make([]map[string]any, 0, 1)
		for _, item := range items {
			if fmt.Sprint(item["id"]) == id {
				filtered = append(filtered, item)
				break
			}
		}
		items = filtered
	}
	if len(items) > *limit {
		items = items[:*limit]
	}
	return writeAICollectionOutput(rt, formatValue, profileName, "memory-records", items)
}

func runAIMemoryDelete(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("ai memory delete", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 || strings.TrimSpace(fs.Args()[0]) == "" {
		return fmt.Errorf("ai memory delete requires one memory record id")
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := gatewayClient(rt, profile).DeleteMemory(ctx, fs.Args()[0], gatewayHeaders(profile, "", "", "memory-privacy-curator", "soha-cli")); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "profile: %s\noperation: memory-deleted\nid: %s\n", profileName, strings.TrimSpace(fs.Args()[0]))
	return err
}

func writeAIOperationOutput(rt Runtime, format, profileName, operation string, item map[string]any) error {
	if format == "json" || format == "yaml" {
		return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(item))
	}
	out := newCheckedWriter(rt.Out)
	out.Printf("profile: %s\noperation: %s\n", profileName, operation)
	writeAISummaryFields(out, item)
	return out.Err()
}

func writeAICollectionOutput(rt Runtime, format, profileName, collection string, items []map[string]any) error {
	if format == "json" || format == "yaml" {
		return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(items))
	}
	out := newCheckedWriter(rt.Out)
	out.Printf("profile: %s\ncollection: %s\ncount: %d\n", profileName, collection, len(items))
	for index, item := range items {
		out.Printf("%d. ", index+1)
		writeAISummaryFields(out, item)
	}
	return out.Err()
}

func writeAISummaryFields(out *checkedWriter, item map[string]any) {
	if len(item) == 0 {
		out.Println("status: accepted")
		return
	}
	fields := []string{"id", "name", "kind", "status", "stage", "decision", "revisionId", "operationId", "runId"}
	wrote := false
	for _, field := range fields {
		value, ok := item[field]
		if !ok || value == nil {
			continue
		}
		if wrote {
			out.Print("\t")
		}
		out.Printf("%s=%v", field, sanitizeCLIValue(value))
		wrote = true
	}
	if !wrote {
		out.Print("status=accepted")
	}
	out.Println()
}

func setQueryValue(query url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		query.Set(key, value)
	}
}
