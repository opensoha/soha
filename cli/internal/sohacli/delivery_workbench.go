package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type deliveryWorkbenchOptions struct {
	profile, output, file, inline             string
	application, environment, binding, bundle string
	search, status, provider, reason          string
	packageID, service, cluster, namespace    string
	phase, sourceMode                         string
	limit                                     int
	page, pageSize                            int
	yes                                       bool
	waitTimeout, pollInterval                 time.Duration
}

type deliveryWorkbenchRequest struct {
	method, path, id string
	body, out        any
	allowed          []string
	list             bool
}

func isDeliveryWorkbench(resource, action string) bool {
	switch resource {
	case "applications", "services", "environments", "environment-catalog", "tasks", "blueprints", "manifests", "manifest-bindings", "manifest-deployments", "executions":
		return true
	case "plans":
		return action == "create" || action == "confirm"
	}
	return false
}

func parseDeliveryWorkbenchOptions(resource, action string, args []string, rt Runtime) (*flag.FlagSet, deliveryWorkbenchOptions, []string, error) {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("delivery "+resource+" "+action, rest, rt)
	var opt deliveryWorkbenchOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.file, "input", "", "typed JSON file, or - for stdin")
	fs.StringVar(&opt.inline, "input-json", "", "typed JSON request")
	fs.StringVar(&opt.application, "application-id", "", "owning application ID")
	fs.StringVar(&opt.environment, "environment-id", "", "base environment ID")
	fs.StringVar(&opt.binding, "application-environment-id", "", "application environment binding ID")
	fs.StringVar(&opt.bundle, "release-bundle-id", "", "release bundle ID")
	fs.StringVar(&opt.search, "search", "", "application name or key search")
	fs.StringVar(&opt.status, "status", "", "task status filter")
	fs.StringVar(&opt.provider, "provider-kind", "", "task provider filter")
	fs.StringVar(&opt.reason, "reason", "", "task control reason, at most 2048 Unicode characters")
	fs.StringVar(&opt.packageID, "package-id", "", "manifest package ID")
	fs.StringVar(&opt.service, "service-id", "", "application service ID")
	fs.StringVar(&opt.cluster, "cluster-id", "", "Soha cluster ID")
	fs.StringVar(&opt.namespace, "namespace", "", "namespace filter")
	fs.StringVar(&opt.phase, "phase", "", "manifest deployment phase")
	fs.StringVar(&opt.sourceMode, "source-mode", "", "manifest deployment source mode")
	fs.IntVar(&opt.limit, "limit", 50, "bounded result count; logs allow 1..1000, other lists 1..200")
	fs.IntVar(&opt.page, "page", 1, "manifest page, at least 1")
	fs.IntVar(&opt.pageSize, "page-size", 20, "manifest page size; packages 1..200, deployments 1..100")
	fs.BoolVar(&opt.yes, "yes", false, "confirm this action; server scope and approval still apply")
	fs.DurationVar(&opt.waitTimeout, "wait-timeout", 10*time.Minute, "wait bound, from 1s to 24h; detaching does not cancel the task")
	fs.DurationVar(&opt.pollInterval, "poll-interval", time.Second, "task poll interval, from 100ms to 30s")
	err := fs.Parse(rest)
	return fs, opt, append(leading, fs.Args()...), err
}

func runDeliveryWorkbench(ctx context.Context, resource, action string, args []string, rt Runtime) error {
	req, opt, err := prepareDeliveryWorkbench(resource, action, args, rt)
	if err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	if req.method != http.MethodGet {
		if err := confirmWorkbenchAction(rt, opt.yes, "Run delivery "+terminalLabel(resource)+" "+terminalLabel(action)+" "+terminalLabel(req.id)+" using the reviewed input?"); err != nil {
			return err
		}
		// A redirect can replay a POST to an old, weaker control endpoint.
		client.Client = client.httpClient()
		client.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if resource == "tasks" && action == "wait" {
		return waitDeliveryTask(ctx, rt, client, req, opt, format)
	}
	if err := executeDeliveryWorkbench(ctx, client, req); err != nil {
		return deliveryWorkbenchError(err, resource, action, req)
	}
	if err := validateDeliveryWorkbenchResult(req); err != nil {
		return err
	}
	if req.out == nil {
		return nil
	}
	if err := writeWorkbenchOutput(rt, client, format, req.out); err != nil {
		return err
	}
	return deliveryWorkbenchOutcome(req.out)
}

func prepareDeliveryWorkbench(resource, action string, args []string, rt Runtime) (deliveryWorkbenchRequest, deliveryWorkbenchOptions, error) {
	fs, opt, ids, err := parseDeliveryWorkbenchOptions(resource, action, args, rt)
	if err != nil {
		return deliveryWorkbenchRequest{}, opt, err
	}
	req, err := buildDeliveryWorkbenchRequest(resource, action, ids, opt)
	if err != nil {
		return deliveryWorkbenchRequest{}, opt, err
	}
	if req.body != nil {
		if resource != "tasks" || action != "cancel" && action != "retry" {
			req.allowed = append(req.allowed, "input", "input-json")
			if err := readTypedWorkbenchInput(rt, opt.file, opt.inline, req.body); err != nil {
				return deliveryWorkbenchRequest{}, opt, err
			}
		}
		if err := validateDeliveryWorkbenchInput(action, req.id, req.body); err != nil {
			return deliveryWorkbenchRequest{}, opt, err
		}
		if input, ok := req.body.(*sohaapi.WorkflowRequestInput); ok {
			req.id = input.IdempotencyKey
		}
		if createdID := deliveryCreationID(req.body); action == "create" && createdID != "" {
			req.id = createdID
		}
	}
	if req.method != http.MethodGet {
		req.allowed = append(req.allowed, "yes")
	}
	if err := checkWorkbenchFlags(fs, req.allowed...); err != nil {
		return deliveryWorkbenchRequest{}, opt, err
	}
	return req, opt, nil
}

func buildDeliveryWorkbenchRequest(resource, action string, ids []string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	if resource == "executions" {
		return deliveryExecutionRequest(action, ids, opt)
	}
	switch resource {
	case "tasks":
		return deliveryTaskRequest(action, ids, opt)
	case "applications", "services", "environments", "environment-catalog":
		return deliveryCatalogRequest(resource, action, ids, opt)
	case "plans", "blueprints":
		return deliveryPlanBlueprintRequest(resource, action, ids)
	case "manifests", "manifest-bindings", "manifest-deployments":
		return deliveryManifestRequest(resource, action, ids, opt)
	}
	return deliveryWorkbenchRequest{}, usageError{message: "unsupported delivery workbench resource"}
}

func deliveryWorkbenchID(ids []string, required bool) (string, error) {
	if !required && len(ids) == 0 {
		return "", nil
	}
	if !required || len(ids) != 1 || len(ids[0]) > 128 || !kubernetesSegment(ids[0]) {
		return "", usageError{message: "this delivery action requires one valid resource ID, or no ID for a collection action"}
	}
	return ids[0], nil
}

func executeDeliveryWorkbench(ctx context.Context, client APIClient, req deliveryWorkbenchRequest) error {
	out := req.out
	if req.list {
		out = &deliveryListResponse{target: req.out}
	}
	return client.doJSON(ctx, req.method, req.path, client.Token, nil, req.body, out)
}

// This adapter is deliberately restricted to Delivery lists whose old Core
// envelope was items. Other Items DTOs and APIClient defaults remain unchanged.
type deliveryListResponse struct{ target any }

func (r *deliveryListResponse) UnmarshalJSON(raw []byte) error {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil || envelope == nil {
		return fmt.Errorf("invalid Delivery list envelope")
	}
	var canonical []byte
	for _, name := range []string{"data", "items"} {
		value, exists := envelope[name]
		if !exists {
			continue
		}
		var rows []any
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		if decoder.Decode(&rows) != nil || rows == nil {
			return fmt.Errorf("delivery list %s must be an array", name)
		}
		normal, _ := json.Marshal(rows)
		if canonical != nil && string(canonical) != string(normal) {
			return fmt.Errorf("delivery list data and items disagree")
		}
		canonical = normal
	}
	if canonical == nil {
		return fmt.Errorf("delivery list lacks data or legacy items")
	}
	envelope["data"] = canonical
	delete(envelope, "items")
	normalized, _ := json.Marshal(envelope)
	return json.Unmarshal(normalized, r.target)
}

func deliveryWorkbenchError(err error, resource, action string, req deliveryWorkbenchRequest) error {
	if req.method == http.MethodGet {
		return err
	}
	var status *httpStatusError
	if errors.As(err, &status) {
		if resource == "tasks" && (action == "cancel" || action == "retry") && (status.StatusCode == 404 || status.StatusCode == 501) {
			return fmt.Errorf("%w; action-scoped task control is unavailable for this task/server; no legacy fallback was attempted", err)
		}
		if status.StatusCode < 500 && status.StatusCode != 408 {
			return err
		}
	}
	if resource == "executions" && action == "trigger" {
		return fmt.Errorf("%w; submission outcome unknown: inspect soha delivery executions request %s using the same profile; do not submit under a new key", err, terminalLabel(req.id))
	}
	return fmt.Errorf("%w; outcome may be uncertain: inspect the original %s ID %s before deciding another action; no automatic replay", err, resource, terminalLabel(req.id))
}

func workbenchDisplayValue(client APIClient, value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("cannot encode workbench response")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, fmt.Errorf("cannot normalize workbench response")
	}
	return redactAIStreamSecrets(sanitizeCLIValue(normalized), client.aiStreamSecrets()), nil
}

func writeWorkbenchOutput(rt Runtime, client APIClient, format string, value any) error {
	clean, err := workbenchDisplayValue(client, value)
	if err != nil {
		return err
	}
	if format == "yaml" {
		// Reuse the renderer directly so its legacy float-normalization step
		// cannot round a reviewed int64 version or generation.
		if err := writeYAMLValue(rt.Out, clean, 0); err != nil {
			return err
		}
		_, err = fmt.Fprintln(rt.Out)
		return err
	}
	return writePrettyJSON(rt.Out, clean)
}

func readTypedWorkbenchInput(rt Runtime, file, inline string, target any) error {
	if (strings.TrimSpace(file) == "") == (strings.TrimSpace(inline) == "") {
		return usageError{message: "provide exactly one of --input FILE|- or --input-json JSON"}
	}
	var reader io.Reader = strings.NewReader(inline)
	if strings.TrimSpace(inline) == "" {
		reader = rt.In
		if file != "-" {
			// #nosec G304 -- this is the caller's explicit request input file.
			source, err := os.Open(file)
			if err != nil {
				return err
			}
			defer func() { _ = source.Close() }()
			reader = source
		}
	}
	raw, err := io.ReadAll(io.LimitReader(reader, (10<<20)+1))
	if err != nil {
		return fmt.Errorf("cannot read workbench input")
	}
	if len(raw) > 10<<20 {
		return usageError{message: "workbench JSON input exceeds 10 MiB"}
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !utf8.Valid(raw) {
		return usageError{message: "workbench input must be a UTF-8 JSON object"}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return usageError{message: "invalid typed workbench input"}
	}
	if decoder.Decode(new(any)) != io.EOF {
		return usageError{message: "workbench input must contain exactly one JSON object"}
	}
	return nil
}

func deliveryFilter(query url.Values, key, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 128 || !kubernetesSegment(value) {
		return usageError{message: "invalid Delivery filter " + key}
	}
	query.Set(key, value)
	return nil
}

func deliveryQuery(path string, query url.Values) string {
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func deliveryUnsupportedAction(resource, action string) error {
	return usageError{message: "unsupported delivery " + resource + " action " + strings.TrimSpace(action)}
}
