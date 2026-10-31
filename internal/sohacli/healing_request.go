package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runHealingOriginalRequest(ctx context.Context, action string, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("observe healing original request", rest, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	policy := fs.String("policy-id", "", "healing policy ID")
	event := fs.String("event-id", "", "owning alert event ID for an original request lookup")
	key := fs.String("idempotency-key", "", "original healing UUID; generated and printed before a new request")
	yes := fs.Bool("yes", false, "confirm request; independent server approval is still required")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 {
		return usageError{message: "heal requires one event ID; request requires one original UUID and --event-id"}
	}
	if action == "heal" {
		if *event != "" {
			return usageError{message: "heal takes the event ID as its positional argument"}
		}
		*event = ids[0]
		if _, err := catalogID(*policy); err != nil {
			return usageError{message: "heal requires --policy-id"}
		}
		if *key == "" {
			*key = uuid.NewString()
		}
	} else {
		if *key != "" || *policy != "" || *yes {
			return usageError{message: "request accepts the original UUID and --event-id, without write flags"}
		}
		*key = ids[0]
	}
	requestID, err := uuid.Parse(*key)
	if err != nil || requestID == uuid.Nil || requestID.String() != *key {
		return usageError{message: "original healing key must be a nonzero canonical UUID"}
	}
	eventPath, err := catalogID(*event)
	if err != nil {
		return usageError{message: "an exact alert event ID is required"}
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	method, path := http.MethodGet, "/api/v1/alert-events/"+eventPath+"/healing-requests"
	var input any
	if action == "heal" {
		if err := confirmWorkbenchAction(rt, *yes, "Request healing for event "+terminalLabel(*event)+" using policy "+terminalLabel(*policy)+" and original UUID "+*key+"?"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(rt.Err, "Healing request: "+*key); err != nil {
			return err
		}
		method, input = http.MethodPost, sohaapi.HealingOriginalRequestInput{IdempotencyKey: requestID, PolicyID: *policy}
	} else {
		path += "/" + *key
	}
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	var receipt sohaapi.HealingOriginalRequestEnvelope
	if err := client.doJSON(ctx, method, path, client.Token, nil, input, &receipt); err != nil {
		return fmt.Errorf("original healing UUID %s; query observe healing-runs request %s --event-id %s before another action; no legacy fallback or automatic resubmission: %w", *key, *key, terminalLabel(*event), err)
	}
	if err := validateHealingReceipt(receipt.Data, requestID, *event, *policy); err != nil {
		return err
	}
	return writeWorkbenchOutput(rt, client, format, map[string]any{"data": receipt.Data.Run})
}

func validateHealingReceipt(receipt sohaapi.HealingOriginalRequestReceipt, key uuid.UUID, event, policy string) error {
	run := receipt.Run
	invalid := func() error {
		return fmt.Errorf("healing receipt is not bound to original UUID %s and event; query the original request before another action", key)
	}
	if receipt.Protocol != "healing-request-v1" || receipt.IdempotencyKey != key || run.EventID != event || (policy != "" && run.PolicyID != policy) || run.ExecutionGeneration < 1 || run.CreatedAt.IsZero() || run.UpdatedAt.IsZero() {
		return invalid()
	}
	for _, id := range []string{run.ID, run.PolicyID, run.Status} {
		if _, err := catalogID(id); err != nil {
			return invalid()
		}
	}
	if len(run.Status) > 64 {
		return invalid()
	}
	if !validHealingDispatch(run) {
		return invalid()
	}
	return nil
}

func validHealingDispatch(run sohaapi.HealingOriginalRun) bool {
	switch string(run.ApprovalStatus) {
	case "pending", "approved", "rejected":
	default:
		return false
	}
	switch string(run.SubmissionStatus) {
	case "not_submitted", "unknown", "accepted":
	default:
		return false
	}
	if run.WorkflowRunID != "" {
		if _, err := catalogID(run.WorkflowRunID); err != nil {
			return false
		}
	}
	if run.SubmissionStatus != "not_submitted" && run.WorkflowRunID == "" {
		return false
	}
	if run.IntentDigest != "" {
		hash := strings.TrimPrefix(run.IntentDigest, "sha256:")
		if len(hash) != 64 || "sha256:"+hash != run.IntentDigest {
			return false
		}
		for _, r := range hash {
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
	}
	if run.SubmissionStatus == "not_submitted" {
		return run.WorkflowRunID == "" && run.ApprovalStatus != "approved"
	}
	return run.ApprovalStatus == "approved"
}
