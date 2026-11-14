package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func vmWorkbenchAction(action string) bool {
	switch action {
	case "list", "get", "detail", "devices", "metrics", "plan", "create", "power", "action", "console":
		return true
	default:
		return false
	}
}

func runVMWorkbench(ctx context.Context, args []string, rt Runtime) error {
	action := args[0]
	leading, rest := extractLeadingPositionals(args[1:], 1)
	fs := newRuntimeFlagSet("compute vms "+action, rest, rt)
	profile := fs.String("profile", "", "profile name")
	file := fs.String("input", "", "typed request JSON file, or - for stdin")
	inline := fs.String("input-json", "", "typed request JSON")
	key := fs.String("idempotency-key", "", "stable operation key; reuse after an uncertain response")
	retryOriginal := fs.Bool("retry-original", false, "resubmit the same create input/key without a second client preflight")
	yes := fs.Bool("yes", false, "confirm the action; server permissions and business checks still apply")
	output := fs.String("output", "json", "output format: json or yaml")
	showConsoleAccess := fs.Bool("show-access-details", false, "explicitly export authorized console access URLs and ticket values")
	provider := fs.String("provider", "", "list filter")
	connection := fs.String("connection-id", "", "list filter")
	namespace := fs.String("namespace", "", "list filter")
	status := fs.String("status", "", "list filter")
	search := fs.String("search", "", "list filter")
	page := fs.Int("page", 1, "list page, starting at 1")
	pageSize := fs.Int("page-size", 50, "list page size, from 1 to 500")
	rangeMinutes := fs.Int("range-minutes", 60, "metrics window, from 1 to 1440 minutes")
	stepSeconds := fs.Int("step-seconds", 60, "metrics sample step, from 1 to 3600 seconds")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	allowed := []string{}
	switch action {
	case "list":
		allowed = []string{"provider", "connection-id", "namespace", "status", "search", "page", "page-size"}
	case "metrics":
		allowed = []string{"range-minutes", "step-seconds"}
	case "console":
		allowed = []string{"show-access-details"}
	case "plan":
		allowed = []string{"input", "input-json"}
	case "create":
		allowed = []string{"input", "input-json", "idempotency-key", "retry-original", "yes"}
	case "power", "action":
		allowed = []string{"input", "input-json", "idempotency-key", "yes"}
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	collection := action == "list" || action == "plan" || action == "create"
	if collection && len(ids) != 0 || !collection && (len(ids) != 1 || !kubernetesSegment(ids[0])) {
		return usageError{message: "list/plan/create take no ID; named operations require one Soha VM ID"}
	}
	if *retryOriginal && action != "create" {
		return usageError{message: "--retry-original is only supported by create"}
	}
	write := action == "create" || action == "power" || action == "action"
	if write && strings.TrimSpace(*key) == "" {
		return usageError{message: "mutations require a stable --idempotency-key"}
	}
	if !write && (*key != "" || *yes) {
		return usageError{message: "--idempotency-key and --yes apply only to mutations"}
	}
	method, path := http.MethodGet, virtualizationPath+"/vms"
	if !collection {
		path += "/" + url.PathEscape(ids[0])
	}
	var body, out any
	switch action {
	case "list":
		if *page < 1 || *pageSize < 1 || *pageSize > 500 {
			return usageError{message: "page must be positive and page-size from 1 to 500"}
		}
		query := url.Values{"page": {strconv.Itoa(*page)}, "pageSize": {strconv.Itoa(*pageSize)}}
		for key, value := range map[string]string{"provider": *provider, "connectionId": *connection, "namespace": *namespace, "status": *status, "search": *search} {
			if value != "" {
				query.Set(key, value)
			}
		}
		path += "?" + query.Encode()
		out = &sohaapi.VirtualMachinePageEnvelope{}
	case "get":
		out = &sohaapi.VirtualMachineEnvelope{}
	case "detail":
		path += "/detail"
		out = &sohaapi.VirtualMachineDetailEnvelope{}
	case "devices":
		path += "/devices"
		out = &sohaapi.VirtualMachineDeviceListEnvelope{}
	case "metrics":
		if *rangeMinutes < 1 || *rangeMinutes > 1440 || *stepSeconds < 1 || *stepSeconds > 3600 {
			return usageError{message: "invalid bounded metrics window or step"}
		}
		path += "/metrics?" + url.Values{"rangeMinutes": {strconv.Itoa(*rangeMinutes)}, "stepSeconds": {strconv.Itoa(*stepSeconds)}}.Encode()
		out = &sohaapi.VirtualMachineMetricsEnvelope{}
	case "console":
		path += "/console"
		out = &sohaapi.VirtualMachineConsoleEnvelope{}
	case "plan", "create":
		method = http.MethodPost
		body = &sohaapi.VirtualMachineCreateInput{}
		if action == "plan" {
			path += "/plan"
			out = &sohaapi.OperationalPlanEnvelope{}
		} else {
			out = &sohaapi.VirtualizationOperationEnvelope{}
		}
	case "power":
		method = http.MethodPost
		path += "/power"
		body = &sohaapi.VirtualMachinePowerInput{}
		out = &sohaapi.VirtualizationOperationEnvelope{}
	case "action":
		method = http.MethodPost
		path += "/actions"
		body = &sohaapi.VirtualMachineActionInput{}
		out = &sohaapi.VirtualizationOperationEnvelope{}
	default:
		return usageError{message: "unsupported VM workbench action"}
	}
	if body != nil {
		if err := readWorkbenchInput(rt, *file, *inline, body); err != nil {
			return err
		}
	} else if *file != "" || *inline != "" {
		return usageError{message: "this read does not accept a request body"}
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	if action == "create" && !*retryOriginal {
		var preview sohaapi.OperationalPlanEnvelope
		if err := client.doJSON(ctx, http.MethodPost, virtualizationPath+"/vms/plan", client.Token, nil, body, &preview); err != nil {
			return err
		}
		if !preview.Data.Ready {
			return fmt.Errorf("VM creation plan is not ready; review the original request before retrying")
		}
		if err := writeStructuredOutput(rt.Err, format, sanitizeCLIJSONValue(preview)); err != nil {
			return err
		}
	}
	if write {
		intent := action
		switch input := body.(type) {
		case *sohaapi.VirtualMachinePowerInput:
			intent = string(input.Action)
		case *sohaapi.VirtualMachineActionInput:
			intent = string(input.Action)
			if err := writeStructuredOutput(rt.Err, format, map[string]any{"action": input.Action, "cpu": input.CPU, "memoryMiB": input.MemoryMiB, "diskGiB": input.DiskGiB, "providerConfigSupplied": input.Config != nil, "review": "Review the original input file for device and provider configuration changes."}); err != nil {
				return err
			}
		}
		if err := confirmWorkbenchAction(rt, *yes, "Run VM "+terminalLabel(intent)+" "+terminalLabel(strings.Join(ids, ""))+" using the reviewed input?"); err != nil {
			return err
		}
	}
	err = client.doJSON(ctx, method, path, client.Token, idempotencyHeaders(*key), body, out)
	if err == nil && action == "console" {
		console, ok := out.(*sohaapi.VirtualMachineConsoleEnvelope)
		if !ok || console == nil {
			return fmt.Errorf("invalid VM console response")
		}
		if *showConsoleAccess {
			return writeStructuredOutput(rt.Out, format, console)
		}
		return writeStructuredOutput(rt.Out, format, map[string]any{"type": console.Data.Type, "ready": console.Data.Ready, "provider": console.Data.Provider, "proxyMode": console.Data.ProxyMode, "accessDetailsHidden": true})
	}
	return writeComputeResult(rt.Out, format, out, err)
}
