package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var secretReferencePattern = regexp.MustCompile(`^(?:secret:[^\s]{1,240}|soha://secrets/[A-Za-z0-9._-]+(?:/versions/[1-9][0-9]*)?)$`)

func runKnowledge(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("knowledge requires a subcommand: search, connectors, sync, or rebuild")
	}
	switch args[0] {
	case "bases", "sources", "documents", "sync-runs", "index-revisions":
		return runKnowledgeCatalog(ctx, args, rt)
	case "search":
		return runKnowledgeSearch(ctx, args[1:], rt)
	case "connectors":
		return runKnowledgeConnectors(ctx, args[1:], rt)
	case "sync":
		return runKnowledgeSync(ctx, args[1:], rt)
	case "rebuild":
		return runKnowledgeRebuild(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown knowledge command %q", args[0])
	}
}

func runKnowledgeConnectors(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("knowledge connectors requires a subcommand: list, create, or validate")
	}
	switch args[0] {
	case "list":
		return runKnowledgeConnectorsList(ctx, args[1:], rt)
	case "create":
		return runKnowledgeConnectorsCreate(ctx, args[1:], rt)
	case "validate":
		return runKnowledgeConnectorsValidate(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown knowledge connectors command %q", args[0])
	}
}

func runKnowledgeConnectorsList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge connectors list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).ListKnowledgeConnectors(ctx, gatewayHeaders(profile, "", "", "knowledge-connector-operator", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAICollectionOutput(rt, formatValue, profileName, "knowledge-connectors", items)
}

func runKnowledgeConnectorsCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge connectors create", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	baseID := fs.String("base-id", "", "knowledge base id")
	name := fs.String("name", "", "connector name")
	legacyID := fs.String("id", "", "deprecated alias for connector name")
	kind := fs.String("kind", "", "connector kind: http, git, or object")
	version := fs.String("version", "", "connector version")
	configRef := fs.String("config-ref", "", "Gateway soha://secrets/<id> reference, optionally /versions/<n>; legacy secret: configuration remains accepted")
	configJSON := fs.String("config-json", "", "connector configuration JSON object")
	allowedHosts := fs.String("allowed-hosts", "", "comma-separated allowed hosts")
	pathPrefixes := fs.String("path-prefixes", "", "comma-separated allowed path prefixes")
	syncMode := fs.String("sync-mode", "manual", "sync mode")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	connectorName, err := resolveDeprecatedAlias(*name, *legacyID, "--name", "--id")
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseID) == "" || connectorName == "" {
		return fmt.Errorf("--base-id and --name are required")
	}
	kindValue := strings.TrimSpace(*kind)
	if kindValue == "object-store" {
		kindValue = "object"
	}
	if kindValue != "http" && kindValue != "git" && kindValue != "object" {
		return fmt.Errorf("--kind must be http, git, or object")
	}
	ref := strings.TrimSpace(*configRef)
	if len(ref) > 512 || !secretReferencePattern.MatchString(ref) {
		return fmt.Errorf("--config-ref must be a canonical Gateway reference or a legacy secret reference")
	}
	config := map[string]any{}
	if strings.TrimSpace(*configJSON) == "" {
		return fmt.Errorf("--config-json is required")
	}
	if err := json.Unmarshal([]byte(*configJSON), &config); err != nil {
		return fmt.Errorf("--config-json must be a JSON object: %w", err)
	}
	if hosts := splitCommaValues(*allowedHosts); len(hosts) > 0 {
		config["allowedHosts"] = hosts
	}
	if prefixes := splitCommaValues(*pathPrefixes); len(prefixes) > 0 {
		config["pathPrefixes"] = prefixes
	}
	mode := strings.TrimSpace(*syncMode)
	if mode == "" {
		return fmt.Errorf("--sync-mode is required")
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	input := KnowledgeConnectorInput{
		KnowledgeBaseID: strings.TrimSpace(*baseID), Name: connectorName,
		Kind: kindValue, Version: strings.TrimSpace(*version), SecretRef: strings.TrimSpace(*configRef),
		Config: config,
	}
	input.SyncPolicy.Mode = mode
	item, err := gatewayClient(rt, profile).CreateKnowledgeConnector(ctx, input, gatewayHeaders(profile, "", "", "knowledge-connector-operator", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "knowledge-connector-created", item)
}

func runKnowledgeConnectorsValidate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge connectors validate", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 || strings.TrimSpace(fs.Args()[0]) == "" {
		return fmt.Errorf("knowledge connectors validate requires one connector id")
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).ValidateKnowledgeConnector(ctx, fs.Args()[0], gatewayHeaders(profile, "", "", "knowledge-connector-operator", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "knowledge-connector-validated", item)
}

func runKnowledgeSync(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("knowledge sync requires a subcommand: start, status, cancel, or retry")
	}
	switch args[0] {
	case "start":
		return runKnowledgeSyncStart(ctx, args[1:], rt)
	case "status", "cancel", "retry":
		return runKnowledgeSyncAction(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown knowledge sync command %q", args[0])
	}
}

func runKnowledgeSyncStart(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge sync start", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	baseID := fs.String("base-id", "", "knowledge base id")
	sourceID := fs.String("source-id", "", "knowledge source id")
	targetRevision := fs.String("target-revision", "", "optional target revision")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*baseID) == "" || strings.TrimSpace(*sourceID) == "" {
		return fmt.Errorf("--base-id and --source-id are required")
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).StartKnowledgeSync(ctx, *baseID, KnowledgeSyncInput{SourceID: strings.TrimSpace(*sourceID), TargetRevision: strings.TrimSpace(*targetRevision)}, gatewayHeaders(profile, "", "", "knowledge-connector-operator", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "knowledge-sync-started", item)
}

func runKnowledgeSyncAction(ctx context.Context, action string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge sync "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 || strings.TrimSpace(fs.Args()[0]) == "" {
		return fmt.Errorf("knowledge sync %s requires one job id", action)
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	var item map[string]any
	if action == "status" {
		item, err = client.GetKnowledgeSync(ctx, fs.Args()[0], gatewayHeaders(profile, "", "", "knowledge-connector-operator", "soha-cli"))
	} else {
		item, err = client.ActOnKnowledgeSync(ctx, fs.Args()[0], action, gatewayHeaders(profile, "", "", "knowledge-connector-operator", "soha-cli"))
	}
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "knowledge-sync-"+action, item)
}

func runKnowledgeRebuild(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge rebuild", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	baseID := fs.String("base-id", "", "knowledge base id")
	reason := fs.String("reason", "", "bounded rebuild reason")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*baseID) == "" {
		return fmt.Errorf("--base-id is required")
	}
	if len(strings.TrimSpace(*reason)) > 512 {
		return fmt.Errorf("--reason must be at most 512 characters")
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).RebuildKnowledgeBase(ctx, *baseID, KnowledgeRebuildInput{Reason: strings.TrimSpace(*reason)}, gatewayHeaders(profile, "", "", "rag-quality-engineer", "soha-cli"))
	if err != nil {
		return err
	}
	return writeAIOperationOutput(rt, formatValue, profileName, "knowledge-rebuild-started", item)
}

func runKnowledgeSearch(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("knowledge search", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	baseIDs := fs.String("base-ids", "", "comma-separated knowledge base ids")
	query := fs.String("query", "", "retrieval query")
	topK := fs.Int("top-k", 5, "maximum results (1-50)")
	sourceIDs := fs.String("source-ids", "", "optional comma-separated source ids")
	documentIDs := fs.String("document-ids", "", "optional comma-separated document ids")
	format := fs.String("output", "summary", "output format: summary, json, or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "summary", "json", "yaml")
	if err != nil {
		return err
	}
	ids := splitCommaValues(*baseIDs)
	if len(ids) == 0 {
		return fmt.Errorf("--base-ids is required")
	}
	if strings.TrimSpace(*query) == "" {
		return fmt.Errorf("--query is required")
	}
	if *topK < 1 || *topK > 50 {
		return fmt.Errorf("--top-k must be between 1 and 50")
	}

	_, profileName, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	input := KnowledgeSearchRequest{
		KnowledgeBaseIDs: ids,
		Query:            strings.TrimSpace(*query),
		TopK:             *topK,
	}
	filters := KnowledgeSearchFilters{
		SourceIDs:   splitCommaValues(*sourceIDs),
		DocumentIDs: splitCommaValues(*documentIDs),
	}
	if len(filters.SourceIDs) > 0 || len(filters.DocumentIDs) > 0 {
		input.Filters = &filters
	}
	result, err := gatewayClient(rt, profile).SearchKnowledge(ctx, input, gatewayHeaders(profile, "", "", "knowledge-researcher", "soha-cli"))
	if err != nil {
		return err
	}

	switch formatValue {
	case "json", "yaml":
		return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(result))
	case "summary":
		return writeKnowledgeSearchSummary(rt, profileName, result)
	default:
		return fmt.Errorf("unsupported output format %q", formatValue)
	}
}

func splitCommaValues(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func writeKnowledgeSearchSummary(rt Runtime, profileName string, result KnowledgeSearchResult) error {
	out := newCheckedWriter(rt.Out)
	out.Printf("profile: %s\nquery: %s\ntrace: %s\n", profileName, result.Query, result.TraceID)
	out.Printf("results: hits=%d candidates=%d timingMs=%d noAnswer=%t\n", len(result.Hits), result.CandidateCount, result.TimingMs, result.NoAnswer)
	for index, hit := range result.Hits {
		uri := hit.Citation.URI
		if uri == "" {
			uri = hit.Citation.Location.URI
		}
		out.Printf("%d. %s\tscore=%.4f\tdocument=%s\tchunk=%s\turi=%s\n", index+1, hit.Title, hit.Score, hit.DocumentID, hit.ChunkID, uri)
		content := strings.TrimSpace(redactSensitiveText(hit.Content))
		if content != "" {
			out.Printf("   %s\n", content)
		}
	}
	return out.Err()
}
