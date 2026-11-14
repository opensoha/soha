package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const virtualizationPath = "/api/v1/virtualization"

func runVMCompute(ctx context.Context, resource string, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("compute %s requires a subcommand", resource)
	}
	if resource == "vms" && vmWorkbenchAction(args[0]) {
		return runVMWorkbench(ctx, args, rt)
	}
	action := args[0]
	args = args[1:]
	if action == "policy" || action == "access" {
		if len(args) == 0 || (args[0] != "get" && args[0] != "set") {
			return fmt.Errorf("compute %s %s requires get or set", resource, action)
		}
		action += " " + args[0]
		args = args[1:]
	}
	leading, args := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("compute "+resource+" "+action, args, rt)
	profile := fs.String("profile", "", "profile name")
	input := fs.String("input", "", "JSON request file or - for stdin")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	method, path := http.MethodGet, virtualizationPath
	var body, result any
	switch resource + " " + action {
	case "capacity check":
		if len(ids) != 0 {
			return fmt.Errorf("capacity check takes --input without a positional ID")
		}
		method, path = http.MethodPost, path+"/capacity/check"
		body, result = &sohaapi.VirtualizationCapacityInput{}, &sohaapi.VirtualizationCapacityEnvelope{}
	case "capacity policy get", "capacity policy set":
		if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" {
			return fmt.Errorf("capacity policy requires a connection ID")
		}
		path += "/clusters/" + url.PathEscape(ids[0]) + "/capacity-policy"
		result = &sohaapi.VirtualizationCapacityPolicyEnvelope{}
		if action == "policy set" {
			method, body = http.MethodPut, &sohaapi.VirtualizationCapacityPolicyInput{}
		}
	case "vms access get", "vms access set", "vms shell":
		if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" {
			return fmt.Errorf("VM access requires a Soha VM ID")
		}
		path += "/vms/" + url.PathEscape(ids[0]) + "/access-binding"
		result = &sohaapi.VirtualMachineAccessBindingEnvelope{}
		if action == "access set" {
			method, body = http.MethodPut, &sohaapi.VirtualMachineAccessBindingInput{}
		}
	default:
		return fmt.Errorf("unknown compute command %s %s", resource, action)
	}
	if body != nil {
		if strings.TrimSpace(*input) == "" {
			return fmt.Errorf("--input is required")
		}
		value, err := readJSONInput(rt, *input, "")
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(body); err != nil {
			return fmt.Errorf("invalid VM input: %w", err)
		}
	} else if *input != "" {
		return fmt.Errorf("--input is only accepted by check and set")
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	if err := client.doJSON(ctx, method, path, client.Token, nil, body, result); err != nil {
		return err
	}
	if action == "shell" {
		binding, ok := result.(*sohaapi.VirtualMachineAccessBindingEnvelope)
		if !ok {
			return fmt.Errorf("invalid VM access binding response")
		}
		return runVMShell(ctx, rt, client, binding.Data)
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(result))
}
