package sohacli

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/google/uuid"
	contractresource "github.com/opensoha/soha/contracts/resource"
)

type remoteProcessExitError struct{ code int64 }

func (e remoteProcessExitError) Error() string {
	return fmt.Sprintf("remote process exited with code %d", e.code)
}

type podExecutionOptions struct {
	Action, Name, Profile, Cluster, Namespace, Key, ID, UID, Container, StdinFile, Output string
	Timeout, Maximum                                                                      int64
	Yes                                                                                   bool
	Argv                                                                                  []string
}

func runKubernetesPodExecution(ctx context.Context, args []string, rt Runtime) error {
	options, err := parsePodExecutionOptions(args, rt)
	if err != nil {
		return err
	}
	format, err := normalizeOutputFormat(options.Output, "json", "yaml", "text")
	if err != nil {
		return err
	}
	client, err := loadComputeClient(ctx, rt, options.Profile)
	if err != nil {
		return err
	}
	if options.Action == "execution" {
		item, err := client.readKubernetesPodExecution(ctx, options.Cluster, options.Namespace, options.Name, options.ID, options.Key)
		return writeKubernetesPodExecution(rt, format, item, err)
	}
	input := contractresource.PodArgvInput{ExpectedUID: options.UID, Container: options.Container, Argv: options.Argv, TimeoutSeconds: options.Timeout, MaxOutputBytes: options.Maximum}
	input, err = input.Normalized()
	if err != nil {
		return usageError{message: err.Error()}
	}
	input.StdinBase64, err = readPodExecutionStdin(rt, options.StdinFile)
	if err != nil {
		return err
	}
	description := "Execute one argv attempt in Pod " + terminalLabel(options.Name) + " in cluster " + terminalLabel(options.Cluster) + " namespace " + terminalLabel(options.Namespace) + " (UID " + terminalLabel(options.UID) + ")?"
	if err := confirmWorkbenchAction(rt, options.Yes, description); err != nil {
		return err
	}
	item, err := client.executeKubernetesPodArgv(ctx, options.Cluster, options.Namespace, options.Name, options.Key, input)
	return writeKubernetesPodExecution(rt, format, item, err)
}

func parsePodExecutionOptions(args []string, rt Runtime) (podExecutionOptions, error) {
	var options podExecutionOptions
	if len(args) < 1 || args[0] != "exec" && args[0] != "execution" {
		return options, usageError{message: "k8s pods requires exec or execution"}
	}
	options.Action = args[0]
	arguments := args[1:]
	if index := slices.Index(arguments, "--"); index >= 0 {
		options.Argv, arguments = arguments[index+1:], arguments[:index]
	}
	leading, rest := extractLeadingPositionals(arguments, 1)
	fs := newRuntimeFlagSet("k8s pods "+options.Action, rest, rt)
	fs.StringVar(&options.Profile, "profile", "", "profile name")
	fs.StringVar(&options.Cluster, "cluster-id", "", "Soha cluster ID")
	fs.StringVar(&options.Namespace, "namespace", "default", "one Kubernetes namespace")
	fs.StringVar(&options.Key, "idempotency-key", "", "stable original execution key; querying it never executes a command")
	fs.StringVar(&options.ID, "execution-id", "", "original receipt UUID, for execution reads")
	fs.StringVar(&options.UID, "expected-uid", "", "UID of the reviewed Pod")
	fs.StringVar(&options.Container, "container", "", "container to freeze for execution")
	fs.Int64Var(&options.Timeout, "exec-timeout-seconds", 10, "bounded process attempt timeout, from 1 to 300")
	fs.Int64Var(&options.Maximum, "max-output-bytes", 65536, "combined stdout/stderr retention limit, up to 1048576")
	fs.StringVar(&options.StdinFile, "stdin-file", "", "optional binary stdin file, or -; maximum 65536 bytes")
	fs.BoolVar(&options.Yes, "yes", false, "confirm one attempt; server authorization still applies")
	fs.StringVar(&options.Output, "output", "json", "output format: json, yaml, or terminal-safe text")
	if err := fs.Parse(rest); err != nil {
		return options, err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 1 {
		return options, usageError{message: "provide one Pod name before the -- argv separator"}
	}
	options.Name = positionals[0]
	allowed := []string{"cluster-id", "namespace", "execution-id", "idempotency-key"}
	if options.Action == "exec" {
		allowed = []string{"cluster-id", "namespace", "idempotency-key", "expected-uid", "container", "exec-timeout-seconds", "max-output-bytes", "stdin-file", "yes"}
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return options, err
	}
	return options, options.validate()
}

func (options podExecutionOptions) validate() error {
	if options.Action == "execution" {
		if len(options.Argv) > 0 {
			return usageError{message: "execution reads never accept argv"}
		}
		return nil
	}
	if !validPodExecutionKey(options.Key) || len(options.Argv) == 0 || options.Timeout < 1 || options.Maximum < 1 {
		return usageError{message: "exec requires --expected-uid, a stable --idempotency-key and -- followed by literal argv; use execution to query an original receipt"}
	}
	if options.StdinFile == "-" && !options.Yes {
		return usageError{message: "--stdin-file - requires --yes so confirmation cannot consume process input"}
	}
	return nil
}

func readPodExecutionStdin(rt Runtime, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	reader := rt.In
	if path != "-" {
		// #nosec G304 -- The user explicitly selects the binary stdin file.
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, contractresource.PodExecMaxStdinBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > contractresource.PodExecMaxStdinBytes {
		return "", usageError{message: "stdin exceeds the 65536 byte limit"}
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func kubernetesPodExecutionPath(cluster, namespace, name string) (string, error) {
	base, err := kubernetesClusterPath(cluster)
	if err != nil {
		return "", err
	}
	if !kubernetesSegment(namespace) || !kubernetesSegment(name) {
		return "", usageError{message: "one valid Pod name and namespace are required"}
	}
	return base + "/workloads/pods/" + url.PathEscape(name) + "/executions", nil
}

func (c APIClient) executeKubernetesPodArgv(ctx context.Context, cluster, namespace, name, key string, input contractresource.PodArgvInput) (contractresource.PodExecutionReceipt, error) {
	var out struct {
		Data contractresource.PodExecutionReceipt `json:"data"`
	}
	path, err := kubernetesPodExecutionPath(cluster, namespace, name)
	if err != nil {
		return out.Data, err
	}
	input, err = input.Normalized()
	if err != nil || !validPodExecutionKey(key) {
		return out.Data, usageError{message: "invalid Pod argv input or original execution key"}
	}
	if c.Timeout <= 0 && (c.Client == nil || c.Client.Timeout <= 0) {
		c.Timeout = time.Duration(input.TimeoutSeconds+20) * time.Second
	}
	err = c.doJSON(ctx, http.MethodPost, kubernetesQuery(path, namespace), c.Token, idempotencyHeaders(key), input, &out)
	if err != nil {
		return out.Data, fmt.Errorf("pod execution response is unavailable; query the original --idempotency-key with k8s pods execution before submitting anything else: %w", err)
	}
	if err := validatePodExecutionReceipt(out.Data, cluster, namespace, name); err != nil {
		return contractresource.PodExecutionReceipt{}, err
	}
	if out.Data.ExpectedUID != input.ExpectedUID || input.Container != "" && out.Data.Container != input.Container || out.Data.Output != nil && out.Data.Output.MaxOutputBytes != input.MaxOutputBytes {
		return contractresource.PodExecutionReceipt{}, fmt.Errorf("pod execution receipt does not match the reviewed original request")
	}
	return out.Data, nil
}

func (c APIClient) readKubernetesPodExecution(ctx context.Context, cluster, namespace, name, id, key string) (contractresource.PodExecutionReceipt, error) {
	var out struct {
		Data contractresource.PodExecutionReceipt `json:"data"`
	}
	path, err := kubernetesPodExecutionPath(cluster, namespace, name)
	if err != nil {
		return out.Data, err
	}
	if (id == "") == (key == "") {
		return out.Data, usageError{message: "provide exactly one --execution-id or original --idempotency-key"}
	}
	if key != "" && !validPodExecutionKey(key) {
		return out.Data, usageError{message: "original execution key must be 1 to 128 printable ASCII characters without spaces"}
	}
	if id != "" {
		parsed, parseErr := uuid.Parse(id)
		if parseErr != nil || parsed.String() != id {
			return out.Data, usageError{message: "--execution-id must be one receipt UUID"}
		}
		path += "/" + id
	}
	if err := c.doJSON(ctx, http.MethodGet, kubernetesQuery(path, namespace), c.Token, idempotencyHeaders(key), nil, &out); err != nil {
		return out.Data, err
	}
	if err := validatePodExecutionReceipt(out.Data, cluster, namespace, name); err != nil {
		return contractresource.PodExecutionReceipt{}, err
	}
	return out.Data, nil
}

func validPodExecutionKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if char < '!' || char > '~' {
			return false
		}
	}
	return true
}

func validatePodExecutionReceipt(item contractresource.PodExecutionReceipt, cluster, namespace, name string) error {
	if !matchesPodExecutionReceipt(item, cluster, namespace, name) {
		return fmt.Errorf("invalid Pod execution receipt; query the original key before any further attempt")
	}
	if item.OutputAvailable != (item.Output != nil) {
		return fmt.Errorf("inconsistent Pod execution output availability")
	}
	if item.Status == "running" {
		if item.FinishedAt != nil || item.OutputAvailable {
			return fmt.Errorf("invalid running Pod execution receipt")
		}
		return nil
	}
	if !slices.Contains([]string{"succeeded", "failed", "timed_out", "canceled", "transport_lost", "unknown"}, item.Status) || item.FinishedAt == nil || item.Output == nil && item.Status != "unknown" {
		return fmt.Errorf("invalid terminal Pod execution receipt")
	}
	if item.Output != nil && (item.Output.Status != item.Status || item.Output.Validate(item.Output.MaxOutputBytes) != nil) {
		return fmt.Errorf("invalid bounded Pod execution output")
	}
	return nil
}

func matchesPodExecutionReceipt(item contractresource.PodExecutionReceipt, cluster, namespace, name string) bool {
	id, err := uuid.Parse(item.ID)
	return err == nil && id.String() == item.ID && item.ClusterID == cluster && item.Namespace == namespace && item.PodName == name && item.ExpectedUID != "" && item.Container != "" && !item.CreatedAt.IsZero() && item.DeadlineAt.After(item.CreatedAt) && item.ExpiresAt.After(item.DeadlineAt)
}

func writeKubernetesPodExecution(rt Runtime, format string, item contractresource.PodExecutionReceipt, requestErr error) error {
	if requestErr != nil {
		return requestErr
	}
	if format == "text" {
		if item.Output != nil {
			for _, stream := range []struct {
				value, encoding string
				writer          io.Writer
			}{{item.Output.Stdout, item.Output.StdoutEncoding, rt.Out}, {item.Output.Stderr, item.Output.StderrEncoding, rt.Err}} {
				data, err := contractresource.DecodePodExecutionStream(stream.value, stream.encoding)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprint(stream.writer, terminalDisplayText(string(data))); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintf(rt.Err, "\nexecution %s: %s\n", item.ID, item.Status); err != nil {
			return err
		}
	} else if err := writeStructuredOutput(rt.Out, format, item); err != nil {
		return err
	}
	if item.Status == "succeeded" {
		return nil
	}
	if item.Status == "failed" && item.Output != nil && item.Output.ExitCode != nil {
		return remoteProcessExitError{code: *item.Output.ExitCode}
	}
	return fmt.Errorf("pod execution is %s; query its original receipt before any further attempt", item.Status)
}
