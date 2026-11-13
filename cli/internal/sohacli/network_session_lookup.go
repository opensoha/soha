package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runNetworkSessionLookup(ctx context.Context, action string, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("network-access sessions "+action, rest, rt)
	var opt catalogOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.session, "session-id", "", "owning network session ID")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	id, err := aiWorkbenchID(append(leading, fs.Args()...), true)
	if err != nil {
		return err
	}
	req, err := networkSessionCommandRequest(action, id, opt)
	if err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	var out sohaapi.NetworkSessionCommandEnvelope
	if err := client.doJSON(ctx, http.MethodGet, req.path, client.Token, nil, nil, &out); err != nil {
		return err
	}
	expectedID := ""
	if action == "command" {
		expectedID, _ = url.PathUnescape(id)
	}
	if err := validateNetworkCommand(out.Data, opt.session, expectedID); err != nil {
		return err
	}
	return writeCatalogOutput(rt, client, format, &out, false)
}

func validateNetworkCommand(command sohaapi.NetworkSessionCommand, session, id string) error {
	if command.ID == "" || command.SessionID != session || id != "" && command.ID != id {
		return fmt.Errorf("unconfirmed network command: response does not match the original session or command ID")
	}
	if _, err := catalogID(command.ID); err != nil {
		return fmt.Errorf("unconfirmed network command: invalid original command ID")
	}
	switch command.Status {
	case "pending", "delivered", "applied", "rejected", "unsupported", "timed-out", "expired":
		return nil
	default:
		return fmt.Errorf("unconfirmed network command: server returned an unknown status")
	}
}
