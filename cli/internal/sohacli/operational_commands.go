package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const defaultPollInterval = 2 * time.Second

func runLogs(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("logs requires query or tail")
	}
	switch args[0] {
	case "query", "tail":
		return runLogsAction(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown logs command %q", args[0])
	}
}

func runLogsAction(ctx context.Context, action string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("logs "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	source := fs.String("source", "cluster", "log source: cluster, docker, or delivery")
	clusterID := fs.String("cluster-id", "", "cluster id")
	projectID := fs.String("project-id", "", "Docker project id")
	applicationID := fs.String("application-id", "", "delivery application id")
	environmentID := fs.String("environment-id", "", "delivery environment id")
	namespace := fs.String("namespace", "", "Kubernetes namespace")
	workloadKind := fs.String("workload-kind", "", "Kubernetes workload kind")
	workloadName := fs.String("workload", "", "Kubernetes workload name")
	podNames := fs.String("pods", "", "comma-separated pod names")
	containers := fs.String("containers", "", "comma-separated container names")
	service := fs.String("service", "", "Docker service name")
	labelSelector := fs.String("selector", "", "Kubernetes label selector")
	allContainers := fs.Bool("all-containers", false, "include every selected container")
	from := fs.String("from", "", "start time in RFC3339")
	to := fs.String("to", "", "end time in RFC3339")
	since := fs.Duration("since", 0, "runtime lookback, for example 10m")
	tail := fs.Int("tail", 100, "maximum recent entries")
	limit := fs.Int("limit", 0, "maximum returned entries")
	text := fs.String("text", "", "message substring")
	severities := fs.String("severities", "", "comma-separated severities")
	cursor := fs.String("cursor", "", "durable query cursor")
	sourceMode := fs.String("source-mode", "auto", "source mode: auto, runtime, or durable")
	direction := fs.String("direction", "backward", "result direction: backward or forward")
	output := fs.String("output", map[bool]string{true: "ndjson", false: "json"}[action == "tail"], "output format: json, yaml, ndjson, or text")
	interval := fs.Duration("interval", defaultPollInterval, "tail polling interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("logs %s does not accept positional arguments", action)
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml", "ndjson", "text")
	if err != nil {
		return err
	}
	query, err := buildLogQuery(logQueryOptions{
		namespace: *namespace, workloadKind: *workloadKind, workloadName: *workloadName,
		podNames: *podNames, containers: *containers, service: *service,
		labelSelector: *labelSelector, allContainers: *allContainers, from: *from, to: *to,
		since: *since, tail: *tail, limit: *limit, text: *text, severities: *severities,
		cursor: *cursor, sourceMode: *sourceMode, direction: *direction,
	})
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	queryLogs, err := selectLogQuery(client, *source, *clusterID, *projectID, *applicationID, *environmentID)
	if err != nil {
		return err
	}
	if action == "query" {
		page, err := queryLogs(ctx, query)
		if err != nil {
			return err
		}
		return writeLogPage(rt.Out, format, page)
	}
	if format != "ndjson" && format != "text" {
		return fmt.Errorf("logs tail supports --output ndjson or text")
	}
	if *interval <= 0 {
		return fmt.Errorf("--interval must be greater than 0")
	}
	return tailLogs(ctx, rt.Out, queryLogs, query, format, *interval)
}

type logQueryOptions struct {
	namespace, workloadKind, workloadName, podNames, containers, service string
	labelSelector, from, to, text, severities, cursor, sourceMode        string
	allContainers                                                        bool
	since                                                                time.Duration
	tail, limit                                                          int
	direction                                                            string
}

func buildLogQuery(options logQueryOptions) (LogQuery, error) {
	if options.tail < 0 || options.tail > 5000 || options.limit < 0 || options.limit > 5000 {
		return LogQuery{}, fmt.Errorf("--tail and --limit must be between 0 and 5000")
	}
	if options.since < 0 {
		return LogQuery{}, fmt.Errorf("--since must not be negative")
	}
	mode := sohaapi.LogSourceMode(strings.ToLower(strings.TrimSpace(options.sourceMode)))
	if !mode.Valid() {
		return LogQuery{}, fmt.Errorf("invalid --source-mode %q", options.sourceMode)
	}
	direction := sohaapi.LogDirection(strings.ToLower(strings.TrimSpace(options.direction)))
	if !direction.Valid() {
		return LogQuery{}, fmt.Errorf("invalid --direction %q", options.direction)
	}
	from, err := optionalRFC3339(options.from)
	if err != nil {
		return LogQuery{}, fmt.Errorf("invalid --from: %w", err)
	}
	to, err := optionalRFC3339(options.to)
	if err != nil {
		return LogQuery{}, fmt.Errorf("invalid --to: %w", err)
	}
	if from != nil && to != nil && from.After(*to) {
		return LogQuery{}, fmt.Errorf("--from must not be after --to")
	}
	query := LogQuery{
		Selector: &LogSourceSelector{
			Namespace: strings.TrimSpace(options.namespace), WorkloadKind: strings.TrimSpace(options.workloadKind),
			WorkloadName: strings.TrimSpace(options.workloadName), PodNames: splitCSV(options.podNames),
			Containers: splitCSV(options.containers), DockerService: strings.TrimSpace(options.service),
			LabelSelector: strings.TrimSpace(options.labelSelector), AllContainers: options.allContainers,
		},
		From: from, To: to, Tail: options.tail, Limit: options.limit, Text: strings.TrimSpace(options.text),
		Severities: splitCSV(options.severities), Cursor: strings.TrimSpace(options.cursor),
		SourceMode: mode, Direction: direction,
	}
	if options.since > 0 {
		query.RuntimeOptions = &sohaapi.LogRuntimeOptions{SinceSeconds: int64((options.since + time.Second - 1) / time.Second)}
	}
	return query, nil
}

func optionalRFC3339(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

type logQueryFunc func(context.Context, LogQuery) (LogPage, error)

func selectLogQuery(client APIClient, source, clusterID, projectID, applicationID, environmentID string) (logQueryFunc, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "cluster":
		if strings.TrimSpace(clusterID) == "" {
			return nil, fmt.Errorf("--cluster-id is required for cluster logs")
		}
		return func(ctx context.Context, query LogQuery) (LogPage, error) {
			return client.QueryClusterLogs(ctx, clusterID, query)
		}, nil
	case "docker", "docker-project":
		if strings.TrimSpace(projectID) == "" {
			return nil, fmt.Errorf("--project-id is required for Docker logs")
		}
		return func(ctx context.Context, query LogQuery) (LogPage, error) {
			return client.QueryDockerProjectLogs(ctx, projectID, query)
		}, nil
	case "delivery", "delivery-environment":
		if strings.TrimSpace(applicationID) == "" || strings.TrimSpace(environmentID) == "" {
			return nil, fmt.Errorf("--application-id and --environment-id are required for delivery logs")
		}
		return func(ctx context.Context, query LogQuery) (LogPage, error) {
			return client.QueryDeliveryEnvironmentLogs(ctx, applicationID, environmentID, query)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported log source %q", source)
	}
}

func writeLogPage(out io.Writer, format string, page LogPage) error {
	page = sanitizeLogPage(page)
	switch format {
	case "json", "yaml":
		return writeStructuredOutput(out, format, page)
	case "ndjson":
		for _, entry := range page.Entries {
			if err := writeNDJSON(out, entry); err != nil {
				return err
			}
		}
		return nil
	case "text":
		for _, entry := range page.Entries {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\n", entry.Timestamp.UTC().Format(time.RFC3339Nano), terminalDisplayText(logSourceLabel(entry)), terminalDisplayText(entry.Message)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported log output %q", format)
	}
}

// Remote log text is data. Keep terminal controls and bidi overrides visible
// without letting them change the terminal or forge additional output rows.
func terminalDisplayText(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069' {
			quoted := strconv.QuoteRuneToASCII(r)
			out.WriteString(quoted[1 : len(quoted)-1])
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func tailLogs(ctx context.Context, out io.Writer, queryLogs logQueryFunc, query LogQuery, format string, interval time.Duration) error {
	query.Direction = sohaapi.LogDirectionForward
	var last time.Time
	seen := map[string]struct{}{}
	for {
		page, err := queryLogs(ctx, query)
		if err != nil {
			return err
		}
		entries, nextLast, nextSeen := newLogEntries(page.Entries, last, seen)
		page.Entries = entries
		if err := writeLogPage(out, format, page); err != nil {
			return err
		}
		if !nextLast.IsZero() {
			last, seen, query.From = nextLast, nextSeen, &nextLast
			query.Tail = 0
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func newLogEntries(entries []LogEntry, last time.Time, seen map[string]struct{}) ([]LogEntry, time.Time, map[string]struct{}) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Timestamp.Before(entries[j].Timestamp) })
	out := make([]LogEntry, 0, len(entries))
	nextLast := last
	nextSeen := seen
	for _, entry := range entries {
		signature := logEntrySignature(entry)
		if entry.Timestamp.Before(last) || (entry.Timestamp.Equal(last) && containsSignature(seen, signature)) {
			continue
		}
		out = append(out, entry)
		switch {
		case entry.Timestamp.After(nextLast):
			nextLast = entry.Timestamp
			nextSeen = map[string]struct{}{signature: {}}
		case entry.Timestamp.Equal(nextLast):
			nextSeen[signature] = struct{}{}
		}
	}
	return out, nextLast, nextSeen
}

func containsSignature(items map[string]struct{}, value string) bool {
	_, ok := items[value]
	return ok
}

func logEntrySignature(entry LogEntry) string {
	return entry.Timestamp.Format(time.RFC3339Nano) + "\x00" + entry.Source.PodName + "\x00" + entry.Source.ContainerName + "\x00" + entry.Source.DockerService + "\x00" + entry.Message
}

func sanitizeLogPage(page LogPage) LogPage {
	for i := range page.Entries {
		page.Entries[i].Message = redactSensitiveText(page.Entries[i].Message)
		for key, value := range page.Entries[i].Attributes {
			page.Entries[i].Attributes[key] = redactSensitiveText(value)
		}
	}
	for i := range page.Warnings {
		page.Warnings[i].Message = redactSensitiveText(page.Warnings[i].Message)
	}
	return page
}

func logSourceLabel(entry LogEntry) string {
	parts := []string{string(entry.Source.Domain)}
	for _, value := range []string{entry.Source.Namespace, entry.Source.PodName, entry.Source.ContainerName, entry.Source.DockerService} {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "/")
}

func writeNDJSON(out io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(raw))
	return err
}

func runOperation(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("operation requires get, wait, or cancel")
	}
	switch args[0] {
	case "get", "wait", "cancel":
		return runOperationAction(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown operation command %q", args[0])
	}
}

func runOperationAction(ctx context.Context, action string, args []string, rt Runtime) error {
	leading, args := extractLeadingPositionals(args, 2)
	fs := newRuntimeFlagSet("operation "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	domainFlag := fs.String("domain", "", "operation domain: virtualization or container_runtime")
	output := fs.String("output", "json", "output format: json, yaml, or summary")
	interval := fs.Duration("interval", defaultPollInterval, "wait polling interval")
	waitTimeout := fs.Duration("wait-timeout", 5*time.Minute, "maximum wait duration")
	reason := fs.String("reason", "", "cancellation reason")
	yes := fs.Bool("yes", false, "skip cancellation confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) < 1 || len(positionals) > 2 {
		return fmt.Errorf("operation %s requires a domain and operation id", action)
	}
	domainValue := strings.TrimSpace(*domainFlag)
	id := strings.TrimSpace(positionals[0])
	if len(positionals) == 2 {
		if domainValue != "" {
			return fmt.Errorf("operation domain may be positional or --domain, not both")
		}
		domainValue, id = positionals[0], strings.TrimSpace(positionals[1])
	}
	domain := ComputeTaskDomain(strings.ToLower(strings.TrimSpace(domainValue)))
	if !domain.Valid() {
		return fmt.Errorf("invalid operation domain %q; use virtualization or container_runtime", domainValue)
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml", "summary")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	switch action {
	case "get":
		item, err := client.GetComputeTask(ctx, domain, id)
		if err != nil {
			return err
		}
		return writeOperationOutput(rt.Out, format, item)
	case "cancel":
		if !*yes {
			confirmed, err := confirmAction(rt, fmt.Sprintf("Cancel %s operation %s?", domain, id))
			if err != nil {
				return err
			}
			if !confirmed {
				return fmt.Errorf("operation cancellation declined; pass --yes for non-interactive use")
			}
		}
		item, err := client.CancelComputeTask(ctx, domain, id, ComputeTaskMutationRequest{Reason: strings.TrimSpace(*reason)})
		if err != nil {
			return err
		}
		return writeOperationOutput(rt.Out, format, item)
	case "wait":
		if *interval <= 0 || *waitTimeout <= 0 {
			return fmt.Errorf("--interval and --wait-timeout must be greater than 0")
		}
		waitCtx, cancel := context.WithTimeout(ctx, *waitTimeout)
		defer cancel()
		item, err := waitForOperation(waitCtx, client, domain, id, *interval)
		if err != nil {
			return err
		}
		if err := writeOperationOutput(rt.Out, format, item); err != nil {
			return err
		}
		if item.NormalizedStatus != sohaapi.ComputeTaskStatusSucceeded {
			return fmt.Errorf("operation %s finished with status %s", id, item.NormalizedStatus)
		}
		return nil
	default:
		return fmt.Errorf("unknown operation command %q", action)
	}
}

func extractLeadingPositionals(args []string, limit int) ([]string, []string) {
	positionals := make([]string, 0, limit)
	for len(args) > 0 && len(positionals) < limit && !strings.HasPrefix(args[0], "-") {
		positionals = append(positionals, args[0])
		args = args[1:]
	}
	return positionals, args
}

func waitForOperation(ctx context.Context, client APIClient, domain ComputeTaskDomain, id string, interval time.Duration) (ComputeTaskView, error) {
	for {
		item, err := client.GetComputeTask(ctx, domain, id)
		if err != nil {
			return ComputeTaskView{}, err
		}
		if operationTerminal(item.NormalizedStatus) {
			return item, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ComputeTaskView{}, fmt.Errorf("wait for operation %s: %w", id, ctx.Err())
		case <-timer.C:
		}
	}
}

func operationTerminal(status ComputeTaskStatus) bool {
	switch status {
	case sohaapi.ComputeTaskStatusSucceeded, sohaapi.ComputeTaskStatusFailed,
		sohaapi.ComputeTaskStatusCanceled, sohaapi.ComputeTaskStatusTimeout:
		return true
	default:
		return false
	}
}

func writeOperationOutput(out io.Writer, format string, item ComputeTaskView) error {
	if format != "summary" {
		return writeStructuredOutput(out, format, item)
	}
	_, err := fmt.Fprintf(out, "operation: %s\ndomain: %s\nstatus: %s\nkind: %s\nsummary: %s\n", item.ID, item.Domain, item.NormalizedStatus, item.Kind, redactSensitiveText(item.Summary))
	return err
}
