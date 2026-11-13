package sohacli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Shared flag/transport plumbing only. Each domain owns a fixed route and DTO
// builder; callers cannot supply an HTTP path or bypass server authorization.
type catalogOptions struct {
	profile, output, file, inline                     string
	base, connection, policy, cluster, status, before string
	rule, event                                       string
	session                                           string
	limit                                             int
	yes                                               bool
	showAccess                                        bool
}

func runCatalogRequest(ctx context.Context, label string, args []string, rt Runtime, build func([]string, catalogOptions) (aiWorkbenchRequest, error)) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet(label, rest, rt)
	var opt catalogOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.file, "input", "", "typed JSON file, or - for stdin")
	fs.StringVar(&opt.inline, "input-json", "", "typed JSON request")
	fs.StringVar(&opt.base, "base-id", "", "owning knowledge base ID")
	fs.StringVar(&opt.connection, "connection-id", "", "directory connection ID")
	fs.StringVar(&opt.policy, "policy-id", "", "healing policy ID")
	fs.StringVar(&opt.cluster, "cluster-id", "", "cluster filter")
	fs.StringVar(&opt.status, "status", "", "status filter")
	fs.StringVar(&opt.before, "before", "", "server pagination cursor")
	fs.StringVar(&opt.rule, "rule-id", "", "alert rule filter")
	fs.StringVar(&opt.event, "event-id", "", "original alert event filter")
	fs.StringVar(&opt.session, "session-id", "", "owning network session ID")
	fs.IntVar(&opt.limit, "limit", 50, "bounded result count, from 1 to 200")
	fs.BoolVar(&opt.yes, "yes", false, "confirm reviewed input; server permission and approval still apply")
	fs.BoolVar(&opt.showAccess, "show-access-details", false, "explicitly export newly issued enrollment or access credentials")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	req, err := build(ids, opt)
	if err != nil {
		return err
	}
	if req.body != nil {
		req.allowed = append(req.allowed, "input", "input-json")
	}
	if req.method != http.MethodGet && !req.readOnly {
		req.allowed = append(req.allowed, "yes")
	}
	if err := checkWorkbenchFlags(fs, req.allowed...); err != nil {
		return err
	}
	if req.body != nil && (!req.optionalBody || opt.file != "" || opt.inline != "") {
		if err := readTypedWorkbenchInput(rt, opt.file, opt.inline, req.body); err != nil {
			return err
		}
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	if req.method != http.MethodGet && !req.readOnly {
		if err := confirmWorkbenchAction(rt, opt.yes, "Run "+terminalLabel(label)+" "+terminalLabel(strings.Join(ids, " "))+" with the reviewed input?"); err != nil {
			return err
		}
	}
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	if err := client.doJSON(ctx, req.method, req.path, client.Token, nil, req.body, req.out); err != nil {
		return err
	}
	return writeCatalogOutput(rt, client, format, req.out, opt.showAccess)
}
func writeCatalogOutput(rt Runtime, client APIClient, format string, value any, showAccess bool) error {
	if value == nil {
		return nil
	}
	if showAccess {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var normalized any
		if err := json.Unmarshal(raw, &normalized); err != nil {
			return err
		}
		return writeStructuredOutput(rt.Out, format, redactAIStreamSecrets(normalized, client.aiStreamSecrets()))
	}
	return writeWorkbenchOutput(rt, client, format, value)
}

func catalogID(value string) (string, error) { return aiWorkbenchID([]string{value}, true) }
