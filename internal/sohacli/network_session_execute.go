package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runNetworkSessionExecute(ctx context.Context, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("network-access sessions execute", rest, rt)
	var opt catalogOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.file, "input", "", "reviewed typed JSON file, or - for stdin")
	fs.StringVar(&opt.inline, "input-json", "", "reviewed typed JSON request")
	fs.BoolVar(&opt.yes, "yes", false, "confirm the original plan; server authorization still applies")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	id, err := aiWorkbenchID(append(leading, fs.Args()...), true)
	if err != nil {
		return err
	}
	var input sohaapi.NetworkSessionActionExecuteInput
	if err := readTypedWorkbenchInput(rt, opt.file, opt.inline, &input); err != nil {
		return err
	}
	if !networkPlanHashPattern.MatchString(input.PlanHash) {
		return usageError{message: "execute requires the original reviewed sha256 planHash"}
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	if err := confirmWorkbenchAction(rt, opt.yes, "Execute the reviewed network session plan for "+terminalLabel(id)+"?"); err != nil {
		return err
	}
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	if _, err := fmt.Fprintf(rt.Err, "Original network plan: %s\n", input.PlanHash); err != nil {
		return err
	}
	var response sohaapi.NetworkSessionCommandEnvelope
	err = client.doJSON(ctx, http.MethodPost, "/api/v1/network-access/sessions/"+id+"/actions/execute", client.Token, nil, input, &response)
	if err != nil {
		return networkSessionUnknown(input.PlanHash, id, err)
	}
	expectedSession, _ := url.PathUnescape(id)
	if validateNetworkCommand(response.Data, expectedSession, "") != nil || response.Data.Action != input.Action {
		return networkSessionUnknown(input.PlanHash, id, fmt.Errorf("server returned an unbound network command receipt"))
	}
	return writeCatalogOutput(rt, client, format, &response, false)
}

func networkSessionUnknown(hash, session string, err error) error {
	return fmt.Errorf("network command outcome is unconfirmed: %w; observe the original request with network-access sessions request %s --session-id %s; do not resubmit execution", err, hash, session)
}
