package sohacli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type directorySyncOptions struct {
	profile, output, connection, path string
	requestID                         uuid.UUID
	yes                               bool
}

func parseDirectorySyncRequest(action string, args []string, rt Runtime) (directorySyncOptions, error) {
	var opt directorySyncOptions
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("directory sync "+action, rest, rt)
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.connection, "connection-id", "", "directory connection ID")
	key := fs.String("idempotency-key", "", "original synchronization UUID; generated and printed before a new start")
	fs.BoolVar(&opt.yes, "yes", false, "confirm synchronization; server permissions still apply")
	if err := fs.Parse(rest); err != nil {
		return opt, err
	}
	ids := append(leading, fs.Args()...)
	if action == "request" {
		if len(ids) != 1 || *key != "" || opt.yes {
			return opt, usageError{message: "sync request requires one original UUID and --connection-id; --yes and --idempotency-key apply only to start"}
		}
		*key = ids[0]
	} else {
		if len(ids) != 0 {
			return opt, usageError{message: "sync start accepts --idempotency-key UUID, without positional IDs"}
		}
		if *key == "" {
			*key = uuid.NewString()
		}
	}
	var err error
	opt.requestID, err = uuid.Parse(*key)
	if err != nil || opt.requestID == uuid.Nil || len(*key) != 36 || !strings.EqualFold(opt.requestID.String(), *key) {
		return opt, usageError{message: "original synchronization key must be a nonzero canonical UUID"}
	}
	opt.path, err = catalogID(opt.connection)
	if err != nil {
		return opt, usageError{message: "this operation requires --connection-id"}
	}
	return opt, nil
}

func runDirectorySyncRequest(ctx context.Context, action string, args []string, rt Runtime) error {
	opt, err := parseDirectorySyncRequest(action, args, rt)
	if err != nil {
		return err
	}
	requestID := opt.requestID
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	method, path := http.MethodGet, "/api/v1/access/directory-connections/"+opt.path+"/sync-requests"
	var input any
	if action == "start" {
		if err := confirmWorkbenchAction(rt, opt.yes, "Synchronize directory "+terminalLabel(opt.connection)+" using original request "+requestID.String()+"?"); err != nil {
			return err
		}
		// The stable reference survives a lost response or interrupted CLI.
		if _, err := fmt.Fprintln(rt.Err, "Directory synchronization request: "+requestID.String()); err != nil {
			return err
		}
		method, input = http.MethodPost, sohaapi.DirectorySyncRequestInput{IdempotencyKey: requestID}
	} else {
		path += "/" + requestID.String()
	}
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	var receipt sohaapi.DirectorySyncRequestEnvelope
	if err := client.doJSON(ctx, method, path, client.Token, nil, input, &receipt); err != nil {
		var status *httpStatusError
		if errors.As(err, &status) && (status.StatusCode == 404 || status.StatusCode == 405 || status.StatusCode == 501) {
			return fmt.Errorf("original synchronization protocol unavailable or request not found; no legacy fallback; original UUID %s: %w", requestID, err)
		}
		return fmt.Errorf("original synchronization UUID %s; query directory sync request %s --connection-id %s before another action: %w", requestID, requestID, terminalLabel(opt.connection), err)
	}
	if err := validateDirectorySyncReceipt(receipt.Data, requestID, opt.connection); err != nil {
		return err
	}
	// Preserve the existing data.id/run projection for scripts; stderr carries
	// the request reference, while the response never invents a completed run.
	if err := writeWorkbenchOutput(rt, client, format, map[string]any{"data": receipt.Data.Run}); err != nil {
		return err
	}
	switch receipt.Data.Run.Status {
	case "failed", "partial", "canceled":
		return fmt.Errorf("original synchronization is %s; inspect run %s; this request was not replayed", receipt.Data.Run.Status, receipt.Data.Run.ID)
	}
	return nil
}

func validateDirectorySyncReceipt(receipt sohaapi.DirectorySyncRequestReceipt, requestID uuid.UUID, connection string) error {
	run := receipt.Run
	if receipt.Protocol != "directory-sync-request-v1" || receipt.IdempotencyKey != requestID || run.ID == uuid.Nil || run.ConnectionID != connection || run.Trigger != "manual" {
		return fmt.Errorf("directory receipt does not match original request %s; query that UUID before another action", requestID)
	}
	switch run.Status {
	case "queued", "running", "succeeded", "partial", "failed", "canceled":
	default:
		return fmt.Errorf("directory receipt lacks a supported original run state")
	}
	if (run.Status == "succeeded" || run.Status == "partial" || run.Status == "failed" || run.Status == "canceled") && run.FinishedAt == nil {
		return fmt.Errorf("directory receipt lacks a durable terminal timestamp")
	}
	return nil
}
