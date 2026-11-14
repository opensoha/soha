package sohacli

import (
	"context"
	"flag"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const copilotPath = "/api/v1/copilot"

type aiWorkbenchOptions struct {
	profile, output, file, inline string
	session, requestID            string
	around, before, after         string
	limit, sequence               int
	yes                           bool
}

type aiWorkbenchRequest struct {
	method, path string
	body, out    any
	allowed      []string
	optionalBody bool
	readOnly     bool
}

func runAIChat(ctx context.Context, args []string, rt Runtime) error {
	if len(args) > 1 && args[0] == "messages" && args[1] == "stream" {
		return runAIChatStream(ctx, args[2:], rt)
	}
	if len(args) > 0 && (args[0] == "request" || args[0] == "native-request") {
		return runAIWorkbench(ctx, "native-request", "get", args[1:], rt)
	}
	if len(args) < 2 || args[0] != "sessions" && args[0] != "messages" {
		return usageError{message: "ai chat requires sessions <list|main|create|get>, messages <list|window|stream>, or request SESSION (native-request remains an alias)"}
	}
	return runAIWorkbench(ctx, "chat-"+args[0], args[1], args[2:], rt)
}

func runAIAssistant(ctx context.Context, args []string, rt Runtime) error {
	if len(args) > 0 && (args[0] == "environment" || args[0] == "runtime" || args[0] == "feed") {
		return runAssistantCatalog(ctx, args, rt)
	}
	if len(args) > 0 && args[0] == "ensure" {
		return runAIWorkbench(ctx, "assistant", "ensure", args[1:], rt)
	}
	if len(args) < 2 {
		return usageError{message: "ai assistant requires ensure, access <get|request>, sessions <list|create>, tasks list, or artifacts list"}
	}
	return runAIWorkbench(ctx, "assistant-"+args[0], args[1], args[2:], rt)
}

func runAIAgentRuns(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return usageError{message: "ai runs requires list, events RUN, cancel RUN, or resume RUN"}
	}
	if args[0] == "events" {
		return runAIRunEvents(ctx, args[1:], rt)
	}
	return runAIWorkbench(ctx, "runs", args[0], args[1:], rt)
}

func parseAIWorkbenchOptions(area, action string, args []string, rt Runtime) (*flag.FlagSet, aiWorkbenchOptions, []string, error) {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("ai "+area+" "+action, rest, rt)
	var opt aiWorkbenchOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.file, "input", "", "typed JSON file, or - for stdin")
	fs.StringVar(&opt.inline, "input-json", "", "typed JSON request")
	fs.StringVar(&opt.session, "session-id", "", "restrict run list to one owned session")
	fs.StringVar(&opt.requestID, "request-id", "", "original turn request ID; never resubmits a message")
	fs.StringVar(&opt.around, "around", "", "window around an owned message ID")
	fs.StringVar(&opt.before, "before", "", "window before an owned message ID")
	fs.StringVar(&opt.after, "after", "", "window after an owned message ID")
	fs.IntVar(&opt.limit, "limit", 40, "message window size, from 3 to 100")
	fs.IntVar(&opt.sequence, "after-sequence", 0, "durable event cursor, at least 0")
	fs.BoolVar(&opt.yes, "yes", false, "confirm this action; server permissions still apply")
	err := fs.Parse(rest)
	return fs, opt, append(leading, fs.Args()...), err
}

func runAIWorkbench(ctx context.Context, area, action string, args []string, rt Runtime) error {
	fs, opt, ids, err := parseAIWorkbenchOptions(area, action, args, rt)
	if err != nil {
		return err
	}
	req, err := buildAIWorkbenchRequest(area, action, ids, opt)
	if err != nil {
		return err
	}
	if req.body != nil {
		req.allowed = append(req.allowed, "input", "input-json")
	}
	if req.method != http.MethodGet {
		req.allowed = append(req.allowed, "yes")
	}
	if err := checkWorkbenchFlags(fs, req.allowed...); err != nil {
		return err
	}
	if req.body != nil && (!req.optionalBody || opt.file != "" || opt.inline != "") {
		if err := readWorkbenchInput(rt, opt.file, opt.inline, req.body); err != nil {
			return err
		}
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	if req.method != http.MethodGet {
		if err := confirmWorkbenchAction(rt, opt.yes, "Run AI "+terminalLabel(area)+" "+terminalLabel(action)+" "+terminalLabel(strings.Join(ids, " "))+" using the reviewed input?"); err != nil {
			return err
		}
	}
	if err := client.doJSON(ctx, req.method, req.path, client.Token, nil, req.body, req.out); err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIJSONValue(req.out))
}

func buildAIWorkbenchRequest(area, action string, ids []string, opt aiWorkbenchOptions) (aiWorkbenchRequest, error) {
	switch area {
	case "chat-sessions", "assistant-sessions":
		return aiSessionRequest(area == "assistant-sessions", action, ids)
	case "chat-messages", "native-request":
		return aiMessageRequest(area, action, ids, opt)
	case "runs":
		return aiRunRequest(action, ids, opt)
	default:
		return aiAssistantRequest(area, action, ids)
	}
}

func aiWorkbenchID(ids []string, required bool) (string, error) {
	if !required && len(ids) == 0 {
		return "", nil
	}
	if !required || len(ids) != 1 || len(ids[0]) > 128 || !kubernetesSegment(ids[0]) {
		return "", usageError{message: "this action requires exactly one valid ID, or no ID for a collection action"}
	}
	return url.PathEscape(ids[0]), nil
}

func aiSessionRequest(personal bool, action string, ids []string) (aiWorkbenchRequest, error) {
	id, err := aiWorkbenchID(ids, action == "get")
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	path := copilotPath + "/sessions"
	if personal {
		path = copilotPath + "/assistant/sessions"
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: path}
	switch {
	case action == "list":
		req.out = &sohaapi.WorkbenchSessionListEnvelope{}
	case action == "create":
		req.method, req.body, req.out = http.MethodPost, &sohaapi.WorkbenchCreateSessionRequest{}, &sohaapi.WorkbenchSessionEnvelope{}
	case action == "main" && !personal:
		req.method, req.path = http.MethodPost, path+"/main"
		req.body, req.out, req.optionalBody = &sohaapi.WorkbenchMainSessionRequest{}, &sohaapi.WorkbenchSessionEnvelope{}, true
	case action == "get" && !personal:
		req.path, req.out = path+"/"+id, &sohaapi.WorkbenchSessionEnvelope{}
	default:
		return aiWorkbenchRequest{}, usageError{message: "unsupported AI session action"}
	}
	return req, nil
}

func aiAssistantRequest(area, action string, ids []string) (aiWorkbenchRequest, error) {
	if _, err := aiWorkbenchID(ids, false); err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: copilotPath + "/assistant"}
	switch {
	case area == "assistant-access" && (action == "get" || action == "request"):
		req.path += "/access"
		req.out = &sohaapi.PersonalAgentAccessEnvelope{}
		if action == "request" {
			req.method = http.MethodPost
		}
	case area == "assistant" && action == "ensure":
		req.method, req.out = http.MethodPost, &sohaapi.PersonalAssistantEnvelope{}
	case area == "assistant-tasks" && action == "list":
		req.path, req.out = req.path+"/tasks", &sohaapi.WorkbenchAgentRunListEnvelope{}
	case area == "assistant-artifacts" && action == "list":
		req.path, req.out = req.path+"/artifacts", &sohaapi.PersonalAssistantArtifactListEnvelope{}
	default:
		return aiWorkbenchRequest{}, usageError{message: "unsupported personal assistant action"}
	}
	return req, nil
}

func aiMessageRequest(area, action string, ids []string, opt aiWorkbenchOptions) (aiWorkbenchRequest, error) {
	id, err := aiWorkbenchID(ids, true)
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: copilotPath + "/sessions/" + id}
	if area == "native-request" {
		if !utf8.ValidString(opt.requestID) || utf8.RuneCountInString(opt.requestID) > 128 || strings.TrimSpace(opt.requestID) == "" {
			return aiWorkbenchRequest{}, usageError{message: "native-request requires the original --request-id of at most 128 characters"}
		}
		req.path += "/native-request?" + url.Values{"requestId": {opt.requestID}}.Encode()
		req.out, req.allowed = &sohaapi.WorkbenchAgentRunEnvelope{}, []string{"request-id"}
		return req, nil
	}
	req.path += "/messages"
	switch action {
	case "list":
		req.out = &sohaapi.WorkbenchMessageListEnvelope{}
	case "window":
		query, err := aiMessageWindowQuery(opt)
		if err != nil {
			return aiWorkbenchRequest{}, err
		}
		req.path += "/window?" + query.Encode()
		req.out, req.allowed = &sohaapi.WorkbenchMessageWindowEnvelope{}, []string{"around", "before", "after", "limit"}
	default:
		return aiWorkbenchRequest{}, usageError{message: "unsupported AI message action"}
	}
	return req, nil
}

func aiMessageWindowQuery(opt aiWorkbenchOptions) (url.Values, error) {
	if opt.limit < 3 || opt.limit > 100 {
		return nil, usageError{message: "window limit must be from 3 to 100"}
	}
	query := url.Values{"limit": {strconv.Itoa(opt.limit)}}
	cursors := 0
	for key, value := range map[string]string{"around": opt.around, "before": opt.before, "after": opt.after} {
		if value == "" {
			continue
		}
		if len(value) > 128 || !kubernetesSegment(value) {
			return nil, usageError{message: "window cursor must be one valid owned message ID"}
		}
		cursors++
		query.Set(key, value)
	}
	if cursors > 1 {
		return nil, usageError{message: "around, before and after are mutually exclusive"}
	}
	return query, nil
}

func aiRunRequest(action string, ids []string, opt aiWorkbenchOptions) (aiWorkbenchRequest, error) {
	id, err := aiWorkbenchID(ids, action != "list")
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: copilotPath + "/agent-runs"}
	switch action {
	case "list":
		if opt.session != "" {
			if _, err := aiWorkbenchID([]string{opt.session}, true); err != nil {
				return aiWorkbenchRequest{}, err
			}
			req.path += "?" + url.Values{"sessionId": {opt.session}}.Encode()
		}
		req.out, req.allowed = &sohaapi.WorkbenchAgentRunListEnvelope{}, []string{"session-id"}
	case "events":
		if opt.sequence < 0 {
			return aiWorkbenchRequest{}, usageError{message: "after-sequence must not be negative"}
		}
		req.path += "/" + id + "/events?" + url.Values{"afterSequence": {strconv.Itoa(opt.sequence)}}.Encode()
		req.out, req.allowed = &sohaapi.AgentRunEventListEnvelope{}, []string{"after-sequence"}
	case "cancel":
		req.method, req.path = http.MethodPost, req.path+"/"+id+"/cancel"
		req.out = &sohaapi.WorkbenchAgentRunEnvelope{}
	case "resume":
		req.method, req.path = http.MethodPost, req.path+"/"+id+"/resume"
		req.body, req.out = &sohaapi.AgentRunResumeRequest{}, &sohaapi.WorkbenchAgentRunEnvelope{}
	default:
		return aiWorkbenchRequest{}, usageError{message: "unsupported AI run action; no single-run GET endpoint exists"}
	}
	return req, nil
}
