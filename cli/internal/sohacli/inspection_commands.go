package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func inspectionEndpoint(action string) (string, string, any, error) {
	const root = "/api/v1/copilot/inspection-tasks"
	switch action {
	case "list":
		return http.MethodGet, root, &sohaapi.WorkbenchInspectionTaskListEnvelope{}, nil
	case "get":
		return http.MethodGet, root + "/{id}", &sohaapi.WorkbenchInspectionTaskEnvelope{}, nil
	case "create":
		return http.MethodPost, root, &sohaapi.WorkbenchInspectionTaskEnvelope{}, nil
	case "update":
		return http.MethodPut, root + "/{id}", &sohaapi.WorkbenchInspectionTaskEnvelope{}, nil
	case "delete":
		return http.MethodDelete, root + "/{id}", nil, nil
	case "run":
		return http.MethodPost, root + "/{id}/execute", &sohaapi.WorkbenchInspectionRunEnvelope{}, nil
	case "runs":
		return http.MethodGet, "/api/v1/copilot/inspection-runs", &sohaapi.WorkbenchInspectionRunListEnvelope{}, nil
	default:
		return "", "", nil, fmt.Errorf("unknown ai inspection action %q", action)
	}
}

type inspectionCall struct {
	TaskID           string                                `json:"taskId,omitempty"`
	Input            *sohaapi.WorkbenchInspectionTaskInput `json:"input,omitempty"`
	IdempotencyKey   string                                `json:"idempotencyKey,omitempty"`
	ExpectedRevision int                                   `json:"expectedRevision,omitempty"`
}

func invokeInspection(ctx context.Context, client APIClient, headers map[string]string, action string, call inspectionCall) (any, error) {
	method, path, output, err := inspectionEndpoint(action)
	if err != nil {
		return nil, err
	}
	if strings.Contains(path, "{id}") {
		if !validDeliveryDocumentID(call.TaskID) {
			return nil, fmt.Errorf("one valid registration id is required")
		}
		path = strings.Replace(path, "{id}", url.PathEscape(call.TaskID), 1)
	} else if call.TaskID != "" && action != "runs" {
		return nil, fmt.Errorf("this action does not accept a registration id")
	}
	if action == "create" || action == "update" {
		if call.Input == nil || !validDeliveryDocumentID(call.Input.ID) {
			return nil, fmt.Errorf("input requires an explicit stable registration id")
		}
		if action == "update" && call.Input.ID != call.TaskID {
			return nil, fmt.Errorf("input id must match the registration")
		}
		if action == "update" && call.Input.CapabilityPlan != nil && call.Input.ExpectedRevision < 1 {
			return nil, fmt.Errorf("capability updates require expectedRevision")
		}
	} else if call.Input != nil {
		return nil, fmt.Errorf("this action does not accept input")
	}
	query := url.Values{}
	if action == "run" {
		if strings.TrimSpace(call.IdempotencyKey) == "" || len(call.IdempotencyKey) > 128 || call.ExpectedRevision < 1 {
			return nil, fmt.Errorf("run requires a stable idempotency key and expected revision")
		}
		query.Set("idempotencyKey", call.IdempotencyKey)
		query.Set("expectedRevision", strconv.Itoa(call.ExpectedRevision))
	} else if call.IdempotencyKey != "" || call.ExpectedRevision != 0 {
		return nil, fmt.Errorf("execution key and revision are only valid for run; updates put expectedRevision in input")
	}
	if action == "runs" && call.TaskID != "" {
		query.Set("taskId", call.TaskID)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var body any
	if call.Input != nil {
		body = call.Input
	}
	if err := client.doJSON(ctx, method, path, client.Token, headers, body, output); err != nil {
		return nil, err
	}
	if output == nil {
		return map[string]any{"deleted": call.TaskID}, nil
	}
	return output, nil
}

func runInspection(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("ai inspection requires list, get, create, update, delete, run, or runs")
	}
	action := args[0]
	if _, _, _, err := inspectionEndpoint(action); err != nil {
		return err
	}
	leading, flags := extractLeadingPositionals(args[1:], 1)
	fs := newRuntimeFlagSet("ai inspection "+action, flags, rt)
	profileFlag := fs.String("profile", "", "profile name")
	file := fs.String("input", "", "WorkbenchInspectionTaskInput JSON file or - for stdin")
	inline := fs.String("input-json", "", "inspection registration JSON")
	formatFlag := fs.String("output", "json", "output format: json or yaml")
	key := fs.String("idempotency-key", "", "reuse the same key after a lost run response")
	revision := fs.Int("expected-revision", 0, "registration revision for manual execution")
	yes := fs.Bool("yes", false, "skip local confirmation; server permissions and approvals remain")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	if len(ids) > 1 {
		return fmt.Errorf("at most one registration id is accepted")
	}
	call := inspectionCall{IdempotencyKey: *key, ExpectedRevision: *revision}
	if len(ids) == 1 {
		call.TaskID = ids[0]
	}
	if action == "create" || action == "update" {
		value, err := readJSONInput(rt, *file, *inline)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		call.Input = &sohaapi.WorkbenchInspectionTaskInput{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(call.Input); err != nil {
			return fmt.Errorf("invalid inspection registration: %w", err)
		}
	} else if *file != "" || *inline != "" {
		return fmt.Errorf("input is only supported for create or update")
	}
	format, err := normalizeOutputFormat(*formatFlag, "json", "yaml")
	if err != nil {
		return err
	}
	if action == "create" || action == "update" || action == "delete" || action == "run" {
		if !*yes {
			confirmed, err := confirmAction(rt, "Run inspection "+action+"? Enabled registrations can execute on future triggers.")
			if err != nil {
				return err
			}
			if !confirmed {
				return fmt.Errorf("inspection action declined; pass --yes for non-interactive use")
			}
		}
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	output, err := invokeInspection(ctx, gatewayClient(rt, profile), gatewayHeaders(profile, "", "", "", "soha-cli"), action, call)
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIJSONValue(output))
}

func (s mcpServer) addInspectionTools(server *mcpsdk.Server) {
	for _, action := range []string{"list", "get", "create", "update", "delete", "run", "runs"} {
		readOnly := action == "list" || action == "get" || action == "runs"
		addCapabilityMCPTool(server, "soha.inspections."+action, "Manage explicitly registered capability inspections. Enabled schedule/alert registrations persist future triggers. Use a fixed registration id and expectedRevision for updates; manual runs require a stable idempotencyKey and expectedRevision. A handed_off receipt links report.capabilityTaskId to soha.tasks.get; inspect evidence there. Disable registrations to stop future triggers; cancel active goals separately.", readOnly, func(ctx context.Context, input inspectionCall) (any, error) {
			return invokeInspection(ctx, s.client, s.headers, action, input)
		})
	}
}
