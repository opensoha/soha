package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

var dockerReadServiceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type dockerReadOptions struct {
	profile, output, project, service, target, path string
	tail, limit                                     int
	since, bytes                                    int64
	timeout                                         time.Duration
}

func runDockerRuntimeRead(ctx context.Context, resource, action string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("docker "+resource+" "+action, args, rt)
	var opt dockerReadOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.project, "project-id", "", "Soha project ID")
	fs.StringVar(&opt.service, "service", "", "registered Compose service name")
	fs.StringVar(&opt.target, "target", "", "exact declared container mount target, not a host source")
	fs.StringVar(&opt.path, "path", "", "relative POSIX path within the selected mount")
	fs.IntVar(&opt.tail, "tail-lines", 0, "log lines: default 200, maximum 2000")
	fs.Int64Var(&opt.since, "since-seconds", 0, "relative log window: maximum 604800 seconds")
	fs.IntVar(&opt.limit, "limit", 0, "directory entry cap: default 200, maximum 1000; byte truncation remains explicit")
	fs.Int64Var(&opt.bytes, "limit-bytes", 0, "UTF-8 byte cap: maximum 1 MiB; file default 256 KiB")
	fs.DurationVar(&opt.timeout, "request-timeout", 70*time.Second, "bounded read timeout: 1s..2m")
	if err := fs.Parse(args); err != nil {
		return err
	}
	readAction, suffix, allowed, err := dockerReadCommand(resource, action)
	if err != nil {
		return err
	}
	if err := checkWorkbenchFlags(fs, append(allowed, "project-id", "service", "request-timeout")...); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageError{message: "Docker runtime reads use explicit --project-id and --service, without positional IDs"}
	}
	if err := validateDockerReadOptions(readAction, opt); err != nil {
		return err
	}
	if opt.path == "." {
		opt.path = ""
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	if strings.HasPrefix(client.Token, "soha_gpat_") || strings.HasPrefix(client.Token, "soha_gsat_") {
		return fmt.Errorf("governed_ai_unsupported: this Docker runtime read has no governed Gateway capability; no Docker REST request was sent")
	}
	client.Timeout = opt.timeout
	client.Client = client.httpClient()
	client.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	query := dockerReadQueryValues(opt)
	endpoint := "/api/v1/docker/projects/" + url.PathEscape(opt.project) + "/runtime/v1/" + suffix + "?" + query.Encode()
	result, err := fetchDockerRuntimeRead(ctx, client, endpoint)
	if err != nil {
		return err
	}
	req := protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: readAction, ProjectID: opt.project, Container: result.Container, Target: opt.target, Path: opt.path, TailLines: opt.tail, SinceSeconds: opt.since, Limit: opt.limit, LimitBytes: opt.bytes}
	if result.Container.ServiceName != opt.service || result.ValidateFor(req) != nil {
		return fmt.Errorf("runtime_read_response_invalid: response identity, bounds or completeness differs from the original request")
	}
	// JSON/YAML encode control characters. Keep authorized UTF-8 content exact
	// so returnedBytes/source size remain true; never emit it as terminal input.
	return writeStructuredOutput(rt.Out, format, struct {
		Data protocol.ReadResult `json:"data"`
	}{Data: result})
}

func fetchDockerRuntimeRead(ctx context.Context, client APIClient, endpoint string) (protocol.ReadResult, error) {
	var raw json.RawMessage
	if err := client.doJSON(ctx, http.MethodGet, endpoint, client.Token, nil, nil, &raw); err != nil {
		return protocol.ReadResult{}, err
	}
	var envelope struct {
		Data protocol.ReadResult `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(raw) || decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF {
		return protocol.ReadResult{}, fmt.Errorf("runtime_read_response_invalid: response must be one strict UTF-8 envelope")
	}
	return envelope.Data, nil
}

func dockerReadCommand(resource, action string) (string, string, []string, error) {
	switch {
	case resource == "logs" && action == "get":
		return "logs", "logs", []string{"tail-lines", "since-seconds", "limit-bytes"}, nil
	case resource == "volumes" && action == "list":
		return "volumes", "volumes", nil, nil
	case resource == "files" && action == "list":
		return "files.list", "files", []string{"target", "path", "limit"}, nil
	case resource == "files" && action == "read":
		return "files.read", "file", []string{"target", "path", "limit-bytes"}, nil
	default:
		return "", "", nil, usageError{message: "Docker runtime reads support logs get, volumes list, files list and files read"}
	}
}

func validateDockerReadOptions(action string, opt dockerReadOptions) error {
	if !kubernetesSegment(opt.project) || !protocol.SafeText(opt.project, 128) || !dockerReadServiceName.MatchString(opt.service) || opt.timeout < time.Second || opt.timeout > 2*time.Minute {
		return usageError{message: "provide a valid --project-id and --service; request-timeout must be 1s..2m"}
	}
	if opt.tail < 0 || opt.tail > 2000 || opt.since < 0 || opt.since > 604800 || opt.limit < 0 || opt.limit > 1000 || opt.bytes < 0 || opt.bytes > 1<<20 {
		return usageError{message: "Docker read limit is outside the supported range"}
	}
	if strings.HasPrefix(action, "files.") && (!protocol.ValidMountTarget(opt.target) || !protocol.ValidRelativePath(opt.path)) {
		return usageError{message: "provide a canonical --target and relative POSIX --path without traversal, links or control characters"}
	}
	return nil
}

func dockerReadQueryValues(opt dockerReadOptions) url.Values {
	query := url.Values{"serviceName": {opt.service}}
	for name, value := range map[string]string{"target": opt.target, "path": opt.path} {
		if value != "" {
			query.Set(name, value)
		}
	}
	for name, value := range map[string]int64{"tailLines": int64(opt.tail), "sinceSeconds": opt.since, "limit": int64(opt.limit), "limitBytes": opt.bytes} {
		if value != 0 {
			query.Set(name, strconv.FormatInt(value, 10))
		}
	}
	return query
}
