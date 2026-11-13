package sohacli

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runKubernetes(ctx context.Context, args []string, rt Runtime) error {
	if len(args) > 0 && args[0] == "pods" {
		if len(args) > 1 && (args[1] == "terminal" || args[1] == "terminal-session" || args[1] == "terminal-close") {
			return runKubernetesPodTerminal(ctx, args[1:], rt)
		}
		return runKubernetesPodExecution(ctx, args[1:], rt)
	}
	if len(args) < 2 {
		return usageError{message: "k8s requires clusters, namespaces, resources, workloads, rollout, events, logs, or create and an action"}
	}
	if args[0] == "logs" {
		if args[1] != "query" && args[1] != "tail" {
			return usageError{message: "k8s logs requires query or tail"}
		}
		for _, arg := range args[2:] {
			if arg == "--source" || strings.HasPrefix(arg, "--source=") {
				return usageError{message: "k8s logs always uses the cluster source"}
			}
		}
		return runLogsAction(ctx, args[1], append([]string{"--source", "cluster"}, args[2:]...), rt)
	}
	if args[0] == "resources" {
		return runKubernetesResources(ctx, args[1:], rt)
	}
	if args[0] == "workloads" {
		return runKubernetesWorkloadAction(ctx, args[1:], rt)
	}
	if args[0] == "rollout" {
		return runKubernetesRollout(ctx, args[1:], rt)
	}
	if args[0] == "create" {
		return runKubernetesCreate(ctx, args[1:], rt)
	}
	return runKubernetesInventory(ctx, args, rt)
}

func runKubernetesInventory(ctx context.Context, args []string, rt Runtime) error {
	resource, action := args[0], args[1]
	if action != "list" && (resource != "clusters" || action != "get") {
		return usageError{message: "unsupported Kubernetes inventory action"}
	}
	fs := newRuntimeFlagSet("k8s "+resource+" "+action, args[2:], rt)
	profile := fs.String("profile", "", "profile name")
	cluster := fs.String("cluster-id", "", "Soha cluster ID")
	namespace := fs.String("namespace", "default", "namespace for events")
	limit := fs.Int("limit", 100, "maximum events, from 1 to 1000")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageError{message: "Kubernetes inventory does not accept positional arguments"}
	}
	path := "/api/v1/clusters"
	var out any
	if resource == "clusters" && action == "list" {
		out = &sohaapi.KubernetesClusterListEnvelope{}
	} else {
		base, err := kubernetesClusterPath(*cluster)
		if err != nil {
			return err
		}
		switch resource {
		case "clusters":
			path = base + "/detail"
			out = &sohaapi.KubernetesClusterDetailEnvelope{}
		case "namespaces":
			path = base + "/namespaces"
			out = &sohaapi.KubernetesNamespaceListEnvelope{}
		case "events":
			if *limit < 1 || *limit > 1000 {
				return usageError{message: "--limit must be from 1 to 1000"}
			}
			path = base + "/events?" + url.Values{"namespace": {*namespace}, "limit": {strconv.Itoa(*limit)}}.Encode()
			out = &sohaapi.KubernetesClusterEventListEnvelope{}
		default:
			return usageError{message: "unknown Kubernetes inventory resource"}
		}
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	err = client.doJSON(ctx, http.MethodGet, path, client.Token, nil, nil, out)
	return writeComputeResult(rt.Out, format, out, err)
}

func runKubernetesResources(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return usageError{message: "k8s resources requires list, get, identity, yaml, apply, or delete"}
	}
	action := args[0]
	switch action {
	case "list", "get", "identity", "yaml", "apply", "delete":
	default:
		return usageError{message: "unknown Kubernetes resource action"}
	}
	leading, rest := extractLeadingPositionals(args[1:], 2)
	fs := newRuntimeFlagSet("k8s resources "+action, rest, rt)
	profile := fs.String("profile", "", "profile name")
	cluster := fs.String("cluster-id", "", "Soha cluster ID")
	namespace := fs.String("namespace", "default", "one Kubernetes namespace")
	allNamespaces := fs.Bool("all-namespaces", false, "list every namespace authorized by the server")
	output := fs.String("output", "json", "output format: json or yaml (API envelope); the yaml action also accepts raw manifest output")
	file := fs.String("file", "", "reviewed YAML file, or - for stdin")
	uid := fs.String("expected-uid", "", "UID from the previously reviewed resource identity")
	version := fs.String("expected-resource-version", "", "opaque version from the previously reviewed resource identity")
	dryRun := fs.Bool("dry-run", false, "return a server-side conditional preflight without writing")
	yes := fs.Bool("yes", false, "confirm the selected mutation; server permissions and approvals still apply")
	showSecret := fs.Bool("show-secret-data", false, "explicitly request Secret YAML; requires separate data-view permission")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	want := 2
	if action == "list" {
		want = 1
	}
	if len(positionals) != want {
		return usageError{message: "provide one kind and, for a named operation, one resource name"}
	}
	endpoint, err := kubernetesEndpoint(positionals[0])
	if err != nil {
		return err
	}
	name := ""
	if len(positionals) == 2 {
		name = positionals[1]
	}
	if *allNamespaces {
		if action != "list" {
			return usageError{message: "--all-namespaces is only supported by list"}
		}
		*namespace = ""
	}
	mutation := action == "apply" || action == "delete"
	if !mutation && (*file != "" || *uid != "" || *version != "" || *dryRun || *yes) {
		return usageError{message: "mutation flags are only accepted by apply or delete"}
	}
	if action == "delete" && (*file != "" || *dryRun) {
		return usageError{message: "delete takes identity preconditions without a YAML file or --dry-run"}
	}
	if endpoint.kind == "Secret" && action == "yaml" && !*showSecret {
		return usageError{message: "Secret YAML requires explicit --show-secret-data and server data-view permission"}
	}
	if *showSecret && action != "yaml" {
		return usageError{message: "--show-secret-data is only accepted by the explicit yaml read"}
	}
	formats := []string{"json", "yaml"}
	if action == "yaml" {
		formats = append(formats, "raw")
	}
	format, err := normalizeOutputFormat(*output, formats...)
	if err != nil {
		return err
	}
	client, err := loadComputeClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	if !mutation {
		value, err := client.ReadKubernetesResource(ctx, *cluster, *namespace, endpoint.kind, name, action)
		if action == "yaml" {
			if err != nil {
				return err
			}
			if format == "raw" {
				envelope, ok := value.(*sohaapi.KubernetesResourceYamlEnvelope)
				if !ok || envelope == nil {
					return fmt.Errorf("invalid resource YAML response")
				}
				_, err = io.WriteString(rt.Out, envelope.Data.Content)
				return err
			}
			// YAML is an explicit authorized content read. Heuristic redaction
			// would silently change an exported manifest before a later edit.
			return writeStructuredOutput(rt.Out, format, value)
		}
		return writeComputeResult(rt.Out, format, value, err)
	}
	if *uid == "" || *version == "" {
		return usageError{message: "mutations require --expected-uid and --expected-resource-version; use resources identity and review the target first"}
	}
	input := sohaapi.KubernetesConditionalResourceYamlInput{Preconditions: sohaapi.KubernetesResourceMutationPreconditions{ExpectedUID: *uid, ExpectedResourceVersion: *version}}
	if action == "apply" {
		content, err := readKubernetesManifest(rt, *file)
		if err != nil {
			return err
		}
		input.Content = content
		value, err := client.ConditionalKubernetesResource(ctx, *cluster, *namespace, endpoint.kind, name, "preflight", input)
		if err != nil {
			return err
		}
		plan, ok := value.(*sohaapi.OperationalPlanEnvelope)
		if !ok || plan == nil {
			return fmt.Errorf("invalid resource preflight response; no mutation was submitted")
		}
		if *dryRun {
			return writeComputeResult(rt.Out, format, plan, nil)
		}
		if !plan.Data.Ready {
			return fmt.Errorf("kubernetes preflight is not ready; review field conflicts before applying")
		}
		if err := writeStructuredOutput(rt.Err, format, plan); err != nil {
			return err
		}
	}
	if !*yes {
		confirmed, err := confirmAction(rt, fmt.Sprintf("%s %s/%s in cluster %s namespace %s (UID %s, version %s)?", action, endpoint.kind, terminalLabel(name), terminalLabel(*cluster), terminalLabel(*namespace), terminalLabel(*uid), terminalLabel(*version)))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("kubernetes mutation declined; pass --yes to confirm non-interactively")
		}
	}
	value, err := client.ConditionalKubernetesResource(ctx, *cluster, *namespace, endpoint.kind, name, action, input)
	if err == nil && action == "apply" { // Do not echo manifest content, including Secrets, after a write.
		value = map[string]any{"applied": true, "clusterId": *cluster, "namespace": *namespace, "kind": endpoint.kind, "name": name, "preconditions": input.Preconditions}
	}
	return writeComputeResult(rt.Out, format, value, err)
}

func readKubernetesManifest(rt Runtime, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", usageError{message: "--file YAML or --file - is required"}
	}
	reader := rt.In
	if path != "-" {
		// #nosec G304 -- The caller explicitly selects the manifest file.
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	raw, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return "", err
	}
	if len(raw) > 4<<20 || strings.TrimSpace(string(raw)) == "" {
		return "", usageError{message: "YAML must be nonempty and at most 4 MiB"}
	}
	return string(raw), nil
}

func runKubernetesCreate(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 || (args[0] != "preflight" && args[0] != "execute") {
		return usageError{message: "k8s create requires preflight or execute"}
	}
	fs := newRuntimeFlagSet("k8s create "+args[0], args[1:], rt)
	profile := fs.String("profile", "", "profile name")
	cluster := fs.String("cluster-id", "", "Soha cluster ID")
	namespace := fs.String("namespace", "default", "fallback namespace for manifests that omit it")
	file := fs.String("file", "", "YAML file, or - for stdin")
	key := fs.String("idempotency-key", "", "stable creation request key; reuse this key after a lost response")
	retryOriginal := fs.Bool("retry-original", false, "retry the same content/key directly; the server returns its durable receipt before preflight, or handles the first submission if none exists")
	yes := fs.Bool("yes", false, "confirm creation; server authorization still applies")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageError{message: "creation does not accept positional arguments"}
	}
	base, err := kubernetesClusterPath(*cluster)
	if err != nil {
		return err
	}
	if args[0] == "execute" && strings.TrimSpace(*key) == "" {
		return usageError{message: "execute requires a stable --idempotency-key"}
	}
	if *retryOriginal && args[0] != "execute" {
		return usageError{message: "--retry-original is only supported by execute"}
	}
	content, err := readKubernetesManifest(rt, *file)
	if err != nil {
		return err
	}
	input := sohaapi.KubernetesResourceCreateRequest{Source: sohaapi.KubernetesResourceCreateSourceGlobalYAML, Content: content, DefaultNamespace: *namespace}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	if !*retryOriginal {
		var preview sohaapi.KubernetesResourcePreflightEnvelope
		if err := client.doJSON(ctx, http.MethodPost, base+"/resource-creation/preflight", client.Token, nil, input, &preview); err != nil {
			return err
		}
		if args[0] == "preflight" {
			return writeComputeResult(rt.Out, format, preview, nil)
		}
		if !preview.Data.Ready {
			return fmt.Errorf("kubernetes creation preflight is not ready; after a lost response, retry the original file and idempotency key with --retry-original")
		}
		if err := writeStructuredOutput(rt.Err, format, preview); err != nil {
			return err
		}
	}
	if !*yes {
		confirmed, err := confirmAction(rt, "Create the reviewed manifests in cluster "+terminalLabel(*cluster)+"?")
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("kubernetes creation declined; pass --yes to confirm non-interactively")
		}
	}
	var result sohaapi.KubernetesResourceCreateResultEnvelope
	err = client.doJSON(ctx, http.MethodPost, base+"/resource-creation/execute", client.Token, idempotencyHeaders(*key), input, &result)
	if err := writeComputeResult(rt.Out, format, result, err); err != nil {
		return err
	}
	_, hashErr := hex.DecodeString(result.Data.ContentHash)
	if result.Data.OperationID == "" || len(result.Data.ContentHash) != 64 || strings.ToLower(result.Data.ContentHash) != result.Data.ContentHash || hashErr != nil || !result.Data.Status.Valid() {
		return fmt.Errorf("invalid creation receipt; inspect the original operation and reuse the original idempotency key when recovering")
	}
	if result.Data.Status != sohaapi.KubernetesResourceCreateBatchStatusSucceeded {
		return fmt.Errorf("kubernetes creation is %s; inspect the returned original operation before retrying", result.Data.Status)
	}
	return nil
}
