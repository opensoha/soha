package sohacli

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// Keep strict JSON decoding at the CLI boundary, including generated unions.
type deliveryTriggerFields sohaapi.DeliveryTriggerInput
type deliveryTriggerScheduleFields sohaapi.DeliveryTriggerSchedule
type deliveryTriggerRequest struct {
	deliveryTriggerFields
	Schedule *deliveryTriggerScheduleFields `json:"schedule,omitempty"`
}

func deliveryTriggerEndpoint(action string) (string, string, any, any, error) {
	const base = "/api/v1/delivery/triggers"
	switch action {
	case "list":
		return http.MethodGet, base, nil, &sohaapi.DeliveryTriggerListEnvelope{}, nil
	case "get":
		return http.MethodGet, base + "/{id}", nil, &sohaapi.DeliveryTriggerEnvelope{}, nil
	case "create":
		return http.MethodPost, base, &deliveryTriggerRequest{}, &sohaapi.DeliveryTriggerEnvelope{}, nil
	case "update":
		return http.MethodPut, base + "/{id}", &deliveryTriggerRequest{}, &sohaapi.DeliveryTriggerEnvelope{}, nil
	case "events":
		return http.MethodGet, base + "/{id}/events", nil, &sohaapi.DeliveryTriggerEventListEnvelope{}, nil
	default:
		return "", "", nil, nil, fmt.Errorf("unknown delivery triggers action %q", action)
	}
}

type deliveryTriggerFlags struct {
	page                 deliveryTemplateSourceFlags
	targetKind, targetID string
}

func (f *deliveryTriggerFlags) register(fs *flag.FlagSet, action string) {
	if action == "events" || action == "list" {
		f.page.register(fs, "list")
	}
	if action == "list" {
		fs.StringVar(&f.targetKind, "target-kind", "", "required: template_source or workflow")
		fs.StringVar(&f.targetID, "target-id", "", "required: target source or workflow ID")
	}
}

func (f deliveryTriggerFlags) path(action, path string) (string, error) {
	if action != "list" && action != "events" {
		return path, nil
	}
	path, err := f.page.path("list", path)
	if err != nil || action == "events" {
		return path, err
	}
	if (f.targetKind != "template_source" && f.targetKind != "workflow") || !validDeliveryDocumentID(f.targetID) {
		return "", fmt.Errorf("provide --target-kind template_source or workflow and --target-id")
	}
	return path + "&" + url.Values{"targetKind": {f.targetKind}, "targetId": {f.targetID}}.Encode(), nil
}

func validateDeliveryTriggerInput(action string, v *deliveryTriggerRequest) error {
	if (action == "create" && v.ExpectedRevision != 0) || (action == "update" && v.ExpectedRevision < 1) {
		return fmt.Errorf("trigger creation requires expectedRevision=0; update requires the loaded positive revision")
	}
	if strings.TrimSpace(v.Name) == "" || !validDeliveryDocumentID(v.TargetID) || (v.TargetKind != "template_source" && v.TargetKind != "workflow") {
		return fmt.Errorf("trigger requires name, targetKind and targetId")
	}
	if v.TargetKind == "workflow" && v.WorkflowVersion < 1 {
		return fmt.Errorf("workflow trigger requires a fixed workflowVersion")
	}
	if action == "create" && strings.TrimSpace(v.ServiceAccountToken) == "" {
		return fmt.Errorf("new trigger requires a serviceAccountToken; supply credentials through --input - or a private file")
	}
	return nil
}
