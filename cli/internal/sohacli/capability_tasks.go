package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func capabilityTaskEndpoint(action string) (string, string, any, any, error) {
	const root = "/api/v1/ai-gateway/"
	switch action {
	case "validate":
		return http.MethodPost, root + "plans/validate", &sohaapi.CapabilityTaskInput{}, &sohaapi.CapabilityPlanValidationEnvelope{}, nil
	case "create":
		return http.MethodPost, root + "tasks", &sohaapi.CapabilityTaskInput{}, &sohaapi.CapabilityTaskEnvelope{}, nil
	case "list":
		return http.MethodGet, root + "tasks", nil, &sohaapi.CapabilityTaskListEnvelope{}, nil
	case "get", "wait":
		return http.MethodGet, root + "tasks/{id}", nil, &sohaapi.CapabilityTaskEnvelope{}, nil
	case "cancel":
		return http.MethodPost, root + "tasks/{id}/cancel", nil, &sohaapi.CapabilityTaskEnvelope{}, nil
	case "resume":
		return http.MethodPost, root + "tasks/{id}/resume", &sohaapi.CapabilityTaskRevisionInput{}, &sohaapi.CapabilityTaskEnvelope{}, nil
	default:
		return "", "", nil, nil, fmt.Errorf("unknown ai task command %q", action)
	}
}

func runCapabilityTask(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("ai task requires validate, create, list, get, wait, cancel, or resume")
	}
	action := args[0]
	method, path, input, output, err := capabilityTaskEndpoint(action)
	if err != nil {
		return err
	}
	leading, flags := extractLeadingPositionals(args[1:], 1)
	fs := newRuntimeFlagSet("ai task "+action, flags, rt)
	profileFlag := fs.String("profile", "", "profile name")
	file := fs.String("input", "", "CapabilityTaskInput JSON file, or - for stdin")
	inline := fs.String("input-json", "", "CapabilityTaskInput JSON with a stable idempotencyKey")
	formatFlag := fs.String("output", "json", "output format: json or yaml")
	yes := fs.Bool("yes", false, "skip command confirmation; server policy and approval still apply")
	limit := fs.Int("limit", 20, "maximum tasks, from 1 to 100")
	planVersion := fs.Int("plan-version", 0, "get an archived plan revision; 0 reads the current plan")
	interval := fs.Duration("interval", defaultPollInterval, "status polling interval")
	timeout := fs.Duration("wait-timeout", 10*time.Minute, "client wait timeout; timing out does not cancel the server task")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if strings.Contains(path, "{id}") {
		if len(positionals) != 1 || !validDeliveryDocumentID(positionals[0]) {
			return fmt.Errorf("ai task %s requires one task id", action)
		}
		path = strings.Replace(path, "{id}", url.PathEscape(positionals[0]), 1)
	} else if len(positionals) != 0 {
		return fmt.Errorf("ai task %s does not accept a task id", action)
	}
	if input != nil {
		value, err := readJSONInput(rt, *file, *inline)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, input); err != nil {
			return fmt.Errorf("input does not match the capability task contract")
		}
	}
	if *limit < 1 || *limit > 100 || *interval <= 0 || *timeout <= 0 {
		return fmt.Errorf("limit must be 1..100; interval and wait-timeout must be positive")
	}
	if action == "list" {
		path += "?limit=" + strconv.Itoa(*limit)
	}
	if *planVersion < 0 || (*planVersion != 0 && action != "get") {
		return fmt.Errorf("--plan-version requires get and a positive revision")
	}
	if *planVersion > 0 {
		path += "?planVersion=" + strconv.Itoa(*planVersion)
	}
	format, err := normalizeOutputFormat(*formatFlag, "json", "yaml")
	if err != nil {
		return err
	}
	if (action == "create" || action == "cancel" || action == "resume") && !*yes {
		confirmed, err := confirmAction(rt, "Run AI task "+action+"?")
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("task action declined; pass --yes for non-interactive use")
		}
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	headers := gatewayHeaders(profile, "", "", "", "soha-cli")
	if action == "wait" {
		waitCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		output, err = waitForCapabilityTask(waitCtx, client, headers, path, *interval)
	} else {
		err = client.doJSON(ctx, method, path, client.Token, headers, input, output)
	}
	if err != nil {
		return err
	}
	if err := writeStructuredOutput(rt.Out, format, sanitizeCLIJSONValue(output)); err != nil {
		return err
	}
	if validation, ok := output.(*sohaapi.CapabilityPlanValidationEnvelope); ok && !validation.Data.Valid {
		return fmt.Errorf("capability plan validation failed; review the returned issues")
	}
	if task, ok := output.(*sohaapi.CapabilityTaskEnvelope); action == "wait" && ok && task.Data.Status != "completed" {
		return fmt.Errorf("capability task is %s; review its evidence and child tasks", task.Data.Status)
	}
	return nil
}

func waitForCapabilityTask(ctx context.Context, client APIClient, headers map[string]string, path string, interval time.Duration) (*sohaapi.CapabilityTaskEnvelope, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		var item sohaapi.CapabilityTaskEnvelope
		if err := client.doJSON(ctx, http.MethodGet, path, client.Token, headers, nil, &item); err != nil {
			return nil, err
		}
		switch item.Data.Status {
		case "completed", "failed", "canceled", "inconclusive", "blocked", "waiting_approval":
			return &item, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
