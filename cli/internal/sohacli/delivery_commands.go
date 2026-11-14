package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runDelivery(ctx context.Context, args []string, rt Runtime) error {
	if len(args) >= 2 && isDeliveryWorkbench(args[0], args[1]) {
		return runDeliveryWorkbench(ctx, args[0], args[1], args[2:], rt)
	}
	if len(args) > 0 && args[0] == "documents" {
		return runDeliveryDocuments(ctx, args[1:], rt)
	}
	if len(args) < 2 {
		return fmt.Errorf("delivery requires a resource and action; use soha delivery --help for the supported workbench commands")
	}
	resource, action := args[0], args[1]
	method, path, input, result, err := deliveryEndpoint(resource, action)
	if err != nil {
		return err
	}
	leading, args := extractLeadingPositionals(args[2:], 1)
	fs := newRuntimeFlagSet("delivery "+resource+" "+action, args, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml (API envelope)")
	var inputFile, inputJSON, comment, reason, applicationID, serviceID string
	var yes bool
	var limit int
	sourceFlags := deliveryTemplateSourceFlags{}
	if resource == "template-sources" {
		sourceFlags.register(fs, action)
	}
	triggerFlags := deliveryTriggerFlags{}
	if resource == "triggers" {
		triggerFlags.register(fs, action)
	}
	if deliveryInputFromFile(input) {
		fs.StringVar(&inputFile, "input", "", "request JSON file, or - for stdin")
		fs.StringVar(&inputJSON, "input-json", "", "request JSON; batch creation requires a stable idempotencyKey")
	}
	if method != http.MethodGet {
		fs.BoolVar(&yes, "yes", false, "skip command confirmation; server authorization and approval still apply")
	}
	if action == "approve" || action == "reject" {
		fs.StringVar(&comment, "comment", "", "approval decision comment")
	}
	if action == "cancel" {
		fs.StringVar(&reason, "reason", "", "cancellation reason")
	}
	if resource == "batches" && action == "list" {
		fs.StringVar(&applicationID, "application-id", "", "filter by application")
		fs.StringVar(&serviceID, "service-id", "", "filter by service")
		fs.IntVar(&limit, "limit", 50, "maximum batches, from 1 to 200")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if action == "list" || action == "create" {
		if len(positionals) != 0 {
			return fmt.Errorf("delivery %s %s does not accept an id", resource, action)
		}
	} else {
		if len(positionals) != 1 || !validDeliveryDocumentID(positionals[0]) {
			return fmt.Errorf("delivery %s %s requires one resource id", resource, action)
		}
		path = strings.Replace(path, "{id}", url.PathEscape(positionals[0]), 1)
	}
	if resource == "batches" && action == "list" {
		if limit < 1 || limit > 200 {
			return fmt.Errorf("--limit must be between 1 and 200")
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if applicationID != "" {
			query.Set("applicationId", applicationID)
		}
		if serviceID != "" {
			query.Set("serviceId", serviceID)
		}
		path += "?" + query.Encode()
	}
	if resource == "template-sources" {
		path, err = sourceFlags.path(action, path)
		if err != nil {
			return err
		}
	}
	if err := prepareDeliveryInput(rt, action, input, inputFile, inputJSON, comment, reason); err != nil {
		return err
	}
	if resource == "triggers" {
		path, err = triggerFlags.path(action, path)
		if err != nil {
			return err
		}
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	if method != http.MethodGet && !yes {
		confirmed, err := confirmAction(rt, fmt.Sprintf("Run delivery %s %s %s?", resource, action, strings.Join(positionals, "")))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("delivery action declined; pass --yes for non-interactive use")
		}
	}
	_, _, selected, err := loadRuntimeProfile(ctx, rt, *profile)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, selected)
	if err := client.doJSON(ctx, method, path, client.Token, nil, input, result); err != nil {
		return err
	}
	if result == nil {
		result = map[string]bool{"success": true}
	}
	if err := writeStructuredOutput(rt.Out, format, sanitizeCLIJSONValue(result)); err != nil {
		return err
	}
	if run, ok := result.(*sohaapi.DeliveryTemplateSyncRunEnvelope); ok && (action == "sync" || action == "apply") {
		switch run.Data.Status {
		case "failed", "invalid", "stale":
			return fmt.Errorf("template sync is %s; review the returned run before starting another attempt", run.Data.Status)
		}
	}
	return nil
}

func deliveryEndpoint(resource, action string) (string, string, any, any, error) {
	if resource == "triggers" {
		return deliveryTriggerEndpoint(action)
	}
	if resource == "template-sources" {
		return deliveryTemplateSourceEndpoint(action)
	}
	switch resource + "/" + action {
	case "batches/list":
		return http.MethodGet, "/api/v1/delivery-batches", nil, &sohaapi.DeliveryBatchListEnvelope{}, nil
	case "batches/get":
		return http.MethodGet, "/api/v1/delivery-batches/{id}", nil, &sohaapi.DeliveryBatchEnvelope{}, nil
	case "batches/create":
		return http.MethodPost, "/api/v1/delivery-batches", &sohaapi.DeliveryBatchInput{}, &sohaapi.DeliveryBatchEnvelope{}, nil
	case "batches/cancel":
		return http.MethodPost, "/api/v1/delivery-batches/{id}/cancel", &sohaapi.DeliveryBatchActionInput{}, &sohaapi.DeliveryBatchEnvelope{}, nil
	case "workflows/list":
		return http.MethodGet, "/api/v1/delivery-workflows", nil, &sohaapi.DeliveryWorkflowListEnvelope{}, nil
	case "workflows/get":
		return http.MethodGet, "/api/v1/delivery-workflows/{id}", nil, &sohaapi.DeliveryWorkflowEnvelope{}, nil
	case "workflows/create":
		return http.MethodPost, "/api/v1/delivery-workflows", &sohaapi.DeliveryWorkflowInput{}, &sohaapi.DeliveryWorkflowEnvelope{}, nil
	case "workflows/update":
		return http.MethodPut, "/api/v1/delivery-workflows/{id}", &sohaapi.DeliveryWorkflowInput{}, &sohaapi.DeliveryWorkflowEnvelope{}, nil
	case "plans/get":
		return http.MethodGet, "/api/v1/delivery/plans/{id}", nil, &sohaapi.DeliveryPlanEnvelope{}, nil
	case "plans/approve", "plans/reject":
		return http.MethodPost, "/api/v1/delivery/plans/{id}/approval", &sohaapi.DeliveryPlanApprovalInput{}, &sohaapi.DeliveryPlanEnvelope{}, nil
	default:
		return "", "", nil, nil, fmt.Errorf("unknown delivery command %q", resource+" "+action)
	}
}

func prepareDeliveryInput(rt Runtime, action string, input any, file, inline, comment, reason string) error {
	if deliveryInputFromFile(input) {
		if (strings.TrimSpace(file) == "") == (strings.TrimSpace(inline) == "") {
			return fmt.Errorf("provide exactly one of --input or --input-json")
		}
		value, err := readJSONInput(rt, file, inline)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(input); err != nil {
			return fmt.Errorf("invalid delivery input: %w", err)
		}
	}
	switch value := input.(type) {
	case *deliveryTriggerRequest:
		return validateDeliveryTriggerInput(action, value)
	case *sohaapi.DeliveryBatchInput:
		if strings.TrimSpace(value.IdempotencyKey) == "" {
			return fmt.Errorf("batch input requires a stable idempotencyKey; reuse it when retrying an uncertain response")
		}
		if (value.Definition == nil) == (strings.TrimSpace(value.WorkflowID) == "") {
			return fmt.Errorf("batch input requires exactly one of definition or workflowId")
		}
		if value.WorkflowID != "" && value.WorkflowVersion < 1 {
			return fmt.Errorf("batch input requires workflowVersion")
		}
	case *sohaapi.DeliveryWorkflowInput:
		if action == "update" && value.ExpectedVersion < 1 {
			return fmt.Errorf("workflow update requires expectedVersion")
		}
	case *sohaapi.DeliveryBatchActionInput:
		value.Reason = reason
	case *sohaapi.DeliveryPlanApprovalInput:
		value.Action, value.Comment = sohaapi.DeliveryPlanApprovalInputAction(action), comment
	}
	return validateDeliveryTemplateSourceInput(action, input)
}

func deliveryInputFromFile(input any) bool {
	switch input.(type) {
	case nil, *sohaapi.DeliveryBatchActionInput, *sohaapi.DeliveryPlanApprovalInput:
		return false
	default:
		return true
	}
}
