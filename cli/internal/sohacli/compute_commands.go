package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runCompute(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("compute requires capabilities, overview, access-sources, providers, provider-instances, resources, tasks, capacity, or vms")
	}
	switch args[0] {
	case "capabilities", "overview":
		return runComputeSummary(ctx, args[0], args[1:], rt)
	case "access-sources":
		return runComputeAccessSources(ctx, args[1:], rt)
	case "providers":
		return runComputeProviders(ctx, args[1:], rt)
	case "provider-instances":
		return runComputeProviderInstances(ctx, args[1:], rt)
	case "resources":
		return runComputeResources(ctx, args[1:], rt)
	case "tasks":
		return runComputeTasks(ctx, args[1:], rt)
	case "capacity", "vms":
		return runVMCompute(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown compute command %q", args[0])
	}
}

func runComputeSummary(ctx context.Context, action string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("compute "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("compute %s does not accept positional arguments", action)
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	client, err := loadComputeClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if action == "capabilities" {
		value, err := client.GetComputeCapabilities(ctx)
		return writeComputeResult(rt.Out, format, value, err)
	}
	value, err := client.GetComputeOverview(ctx)
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeAccessSources(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("compute access-sources requires list")
	}
	fs := newRuntimeFlagSet("compute access-sources list", args[1:], rt)
	profileFlag := fs.String("profile", "", "profile name")
	sourceType := fs.String("source-type", "", "source type")
	providerKey := fs.String("provider-key", "", "provider key")
	cursor := fs.String("cursor", "", "pagination cursor")
	limit := fs.Int("limit", 50, "result limit")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("compute access-sources list does not accept positional arguments")
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	value, err := client.ListComputeAccessSources(ctx, sohaapi.ListComputeAccessSourcesParams{
		SourceType:  sohaapi.ComputeAccessSourceType(strings.TrimSpace(*sourceType)),
		ProviderKey: strings.TrimSpace(*providerKey), Cursor: sohaapi.ComputeCursor(strings.TrimSpace(*cursor)), Limit: sohaapi.ComputeLimit(*limit),
	})
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeProviders(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("compute providers requires list")
	}
	fs := newRuntimeFlagSet("compute providers list", args[1:], rt)
	profileFlag := fs.String("profile", "", "profile name")
	domain := fs.String("domain", "", "provider domain")
	source := fs.String("source", "", "provider source")
	cursor := fs.String("cursor", "", "pagination cursor")
	limit := fs.Int("limit", 50, "result limit")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("compute providers list does not accept positional arguments")
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	value, err := client.ListComputeProviders(ctx, sohaapi.ListComputeProvidersParams{
		Domain: sohaapi.ComputeProviderDomain(strings.TrimSpace(*domain)), Source: sohaapi.ComputeProviderSource(strings.TrimSpace(*source)),
		Cursor: sohaapi.ComputeCursor(strings.TrimSpace(*cursor)), Limit: sohaapi.ComputeLimit(*limit),
	})
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeProviderInstances(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("compute provider-instances requires list, get, health, or discover")
	}
	switch args[0] {
	case "list":
		return runComputeProviderInstanceList(ctx, args[1:], rt)
	case "get", "health", "discover":
		return runComputeProviderInstanceAction(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown compute provider-instances command %q", args[0])
	}
}

func runComputeProviderInstanceList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("compute provider-instances list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	domain := fs.String("domain", "", "provider domain")
	providerKey := fs.String("provider-key", "", "provider key")
	cursor := fs.String("cursor", "", "pagination cursor")
	limit := fs.Int("limit", 50, "result limit")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	value, err := client.ListComputeProviderInstances(ctx, sohaapi.ListComputeProviderInstancesParams{
		Domain: sohaapi.ComputeProviderDomain(strings.TrimSpace(*domain)), ProviderKey: strings.TrimSpace(*providerKey),
		Cursor: sohaapi.ComputeCursor(strings.TrimSpace(*cursor)), Limit: sohaapi.ComputeLimit(*limit),
	})
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeProviderInstanceAction(ctx context.Context, action string, args []string, rt Runtime) error {
	leading, args := extractLeadingPositionals(args, 3)
	fs := newRuntimeFlagSet("compute provider-instances "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	generation := fs.Int64("generation", 0, "expected provider generation")
	scope := fs.String("scope", "", "provider scope")
	maxItems := fs.Int("max-items", 1000, "maximum discovered items")
	idempotencyKey := fs.String("idempotency-key", "", "stable idempotency key")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 3 {
		return fmt.Errorf("compute provider-instances %s requires domain, provider key, and instance ref", action)
	}
	domain := sohaapi.ComputeProviderDomain(strings.TrimSpace(positionals[0]))
	if !domain.Valid() {
		return fmt.Errorf("invalid provider domain %q", positionals[0])
	}
	if action != "get" && *generation < 1 {
		return fmt.Errorf("--generation must be greater than 0")
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	providerKey, instanceRef := strings.TrimSpace(positionals[1]), strings.TrimSpace(positionals[2])
	if action == "get" {
		value, err := client.GetComputeProviderInstance(ctx, domain, providerKey, instanceRef)
		return writeComputeResult(rt.Out, format, value, err)
	}
	key := strings.TrimSpace(*idempotencyKey)
	if key == "" {
		key = computeIdempotencyKey(string(domain), instanceRef, action, map[string]any{"generation": *generation, "scope": strings.TrimSpace(*scope), "maxItems": *maxItems})
	}
	if action == "health" {
		value, err := client.CheckComputeProviderInstanceHealth(ctx, domain, providerKey, instanceRef, key, sohaapi.ComputeProviderReadRequest{ExpectedGeneration: *generation, Scope: strings.TrimSpace(*scope)})
		return writeComputeResult(rt.Out, format, value, err)
	}
	value, err := client.DiscoverComputeProviderInstance(ctx, domain, providerKey, instanceRef, key, sohaapi.ComputeProviderDiscoverRequest{ExpectedGeneration: *generation, Scope: strings.TrimSpace(*scope), MaxItems: *maxItems})
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeResources(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 || (args[0] != "get" && args[0] != "relations" && args[0] != "action") {
		return fmt.Errorf("compute resources requires get, relations, or action")
	}
	action := args[0]
	count := 3
	if action == "action" {
		count = 4
	}
	leading, args := extractLeadingPositionals(args[1:], count)
	fs := newRuntimeFlagSet("compute resources "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	cursor := fs.String("cursor", "", "pagination cursor")
	limit := fs.Int("limit", 50, "result limit")
	reason := fs.String("reason", "", "operation reason")
	inputJSON := fs.String("input-json", "", "resource action input JSON")
	idempotencyKey := fs.String("idempotency-key", "", "stable idempotency key")
	yes := fs.Bool("yes", false, "skip action confirmation")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != count {
		return fmt.Errorf("compute resources %s requires domain, kind, id%s", action, map[bool]string{true: ", and action", false: ""}[action == "action"])
	}
	domain := sohaapi.ComputeDomain(strings.TrimSpace(positionals[0]))
	kind := sohaapi.ComputeResourceKind(strings.TrimSpace(positionals[1]))
	if !domain.Valid() || !kind.Valid() {
		return fmt.Errorf("invalid compute resource domain or kind")
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(positionals[2])
	if action == "get" {
		value, err := client.GetComputeResource(ctx, domain, kind, id)
		return writeComputeResult(rt.Out, format, value, err)
	}
	if action == "relations" {
		value, err := client.ListComputeResourceRelations(ctx, domain, kind, id, sohaapi.ListComputeResourceRelationsParams{Cursor: sohaapi.ComputeCursor(strings.TrimSpace(*cursor)), Limit: sohaapi.ComputeLimit(*limit)})
		return writeComputeResult(rt.Out, format, value, err)
	}
	requestedAction := strings.TrimSpace(positionals[3])
	if !*yes {
		confirmed, err := confirmAction(rt, fmt.Sprintf("Run %s on %s %s?", requestedAction, kind, id))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("compute resource action declined; pass --yes for non-interactive use")
		}
	}
	input := sohaapi.ComputeResourceActionRequest{Reason: strings.TrimSpace(*reason)}
	if strings.TrimSpace(*inputJSON) != "" {
		if err := json.Unmarshal([]byte(*inputJSON), &input); err != nil {
			return fmt.Errorf("decode --input-json: %w", err)
		}
		if strings.TrimSpace(*reason) != "" {
			input.Reason = strings.TrimSpace(*reason)
		}
	}
	key := strings.TrimSpace(*idempotencyKey)
	if key == "" {
		key = computeIdempotencyKey(string(domain), id, requestedAction, input)
	}
	value, err := client.ExecuteComputeResourceAction(ctx, domain, kind, id, requestedAction, key, input)
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeTasks(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("compute tasks requires list, get, logs, cancel, retry, or wait")
	}
	if args[0] == "get" || args[0] == "cancel" || args[0] == "wait" {
		return runOperationAction(ctx, args[0], args[1:], rt)
	}
	switch args[0] {
	case "list":
		return runComputeTaskList(ctx, args[1:], rt)
	case "logs", "retry":
		return runComputeTaskAction(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown compute tasks command %q", args[0])
	}
}

func runComputeTaskList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("compute tasks list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	domain := fs.String("domain", "", "task domain")
	providerKey := fs.String("provider-key", "", "provider key")
	status := fs.String("status", "", "task status")
	category := fs.String("category", "", "task category")
	resourceKind := fs.String("resource-kind", "", "resource kind")
	resourceID := fs.String("resource-id", "", "resource id")
	sortBy := fs.String("sort-by", "", "sort field")
	sortOrder := fs.String("sort-order", "", "sort order")
	cursor := fs.String("cursor", "", "pagination cursor")
	limit := fs.Int("limit", 50, "result limit")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	value, err := client.ListComputeTasks(ctx, sohaapi.ListComputeTasksParams{
		Domain: sohaapi.ComputeTaskDomain(strings.TrimSpace(*domain)), ProviderKey: strings.TrimSpace(*providerKey),
		Status: sohaapi.ComputeTaskStatus(strings.TrimSpace(*status)), Category: sohaapi.ComputeTaskCategory(strings.TrimSpace(*category)),
		ResourceKind: strings.TrimSpace(*resourceKind), ResourceID: strings.TrimSpace(*resourceID), SortBy: strings.TrimSpace(*sortBy),
		SortOrder: sohaapi.ListComputeTasksParamsSortOrder(strings.TrimSpace(*sortOrder)), Cursor: sohaapi.ComputeCursor(strings.TrimSpace(*cursor)), Limit: sohaapi.ComputeLimit(*limit),
	})
	return writeComputeResult(rt.Out, format, value, err)
}

func runComputeTaskAction(ctx context.Context, action string, args []string, rt Runtime) error {
	leading, args := extractLeadingPositionals(args, 2)
	fs := newRuntimeFlagSet("compute tasks "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	reason := fs.String("reason", "", "retry reason")
	idempotencyKey := fs.String("idempotency-key", "", "stable idempotency key")
	yes := fs.Bool("yes", false, "skip retry confirmation")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 2 {
		return fmt.Errorf("compute tasks %s requires domain and task id", action)
	}
	domain := ComputeTaskDomain(strings.TrimSpace(positionals[0]))
	if !domain.Valid() {
		return fmt.Errorf("invalid task domain %q", positionals[0])
	}
	format, client, err := computeOutputClient(ctx, rt, *profileFlag, *output)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(positionals[1])
	if action == "logs" {
		value, err := client.ListComputeTaskLogs(ctx, domain, id)
		return writeComputeResult(rt.Out, format, value, err)
	}
	if !*yes {
		confirmed, err := confirmAction(rt, fmt.Sprintf("Retry %s task %s?", domain, id))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("compute task retry declined; pass --yes for non-interactive use")
		}
	}
	input := ComputeTaskMutationRequest{Reason: strings.TrimSpace(*reason)}
	key := strings.TrimSpace(*idempotencyKey)
	if key == "" {
		key = computeIdempotencyKey(string(domain), id, "retry", input)
	}
	value, err := client.RetryComputeTaskWithKey(ctx, domain, id, key, input)
	return writeComputeResult(rt.Out, format, value, err)
}

func loadComputeClient(ctx context.Context, rt Runtime, profileName string) (APIClient, error) {
	_, _, profile, err := loadRuntimeProfile(ctx, rt, profileName)
	if err != nil {
		return APIClient{}, err
	}
	return gatewayClient(rt, profile), nil
}

func computeOutputClient(ctx context.Context, rt Runtime, profileName, output string) (string, APIClient, error) {
	format, err := normalizeOutputFormat(output, "json", "yaml")
	if err != nil {
		return "", APIClient{}, err
	}
	client, err := loadComputeClient(ctx, rt, profileName)
	return format, client, err
}

func writeComputeResult(out io.Writer, format string, value any, err error) error {
	if err != nil {
		return err
	}
	return writeStructuredOutput(out, format, sanitizeCLIJSONValue(value))
}
