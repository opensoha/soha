package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	contractresource "github.com/opensoha/soha-contracts/resource"
)

func (c APIClient) ExecuteKubernetesWorkloadAction(ctx context.Context, cluster, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	var empty contractresource.ConditionalWorkloadActionReceipt
	base, err := kubernetesClusterPath(cluster)
	if err != nil {
		return empty, err
	}
	if !kubernetesSegment(name) || !kubernetesSegment(namespace) {
		return empty, usageError{message: "one valid name and namespace are required"}
	}
	if err := input.Validate(kind, action); err != nil {
		return empty, usageError{message: err.Error()}
	}
	var out struct {
		Data contractresource.ConditionalWorkloadActionReceipt `json:"data"`
	}
	path := base + "/resources/" + kind + "/" + url.PathEscape(name) + "/conditional/actions/" + action
	if err := c.doJSON(ctx, http.MethodPost, kubernetesQuery(path, namespace), c.Token, nil, input, &out); err != nil {
		return empty, err
	}
	item := out.Data
	if item.Kind != kind || item.Name != name || item.Namespace != namespace || item.Action != action || item.Preconditions != input.Preconditions || item.UID != input.Preconditions.ExpectedUID || item.ResourceVersion == "" || item.DryRun != input.DryRun || item.Applied == input.DryRun {
		return empty, fmt.Errorf("invalid conditional workload receipt; inspect the original target before retrying")
	}
	return item, nil
}

func runKubernetesWorkloadAction(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 || args[0] != "restart" && args[0] != "scale" && args[0] != "rollback" {
		return usageError{message: "k8s workloads requires restart, scale, or rollback"}
	}
	action := args[0]
	leading, rest := extractLeadingPositionals(args[1:], 2)
	fs := newRuntimeFlagSet("k8s workloads "+action, rest, rt)
	profile := fs.String("profile", "", "profile name")
	cluster := fs.String("cluster-id", "", "Soha cluster ID")
	namespace := fs.String("namespace", "default", "one Kubernetes namespace")
	uid := fs.String("expected-uid", "", "UID of the reviewed workload")
	version := fs.String("expected-resource-version", "", "opaque version of the reviewed workload")
	replicas := fs.String("replicas", "", "nonnegative replica count for scale")
	revision := fs.String("revision", "", "positive Deployment revision for rollback")
	dryRun := fs.Bool("dry-run", false, "perform a Kubernetes dry-run with the same identity and action permission")
	yes := fs.Bool("yes", false, "confirm the reviewed action; server authorization still applies")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 2 {
		return usageError{message: "provide one workload kind and one name"}
	}
	endpoint, err := kubernetesEndpoint(positionals[0])
	if err != nil {
		return err
	}
	input := contractresource.ConditionalWorkloadActionInput{Preconditions: contractresource.ResourceMutationPreconditions{ExpectedUID: *uid, ExpectedResourceVersion: *version}, Revision: *revision, DryRun: true}
	if *replicas != "" {
		value, err := strconv.ParseInt(*replicas, 10, 32)
		if err != nil || value < 0 {
			return usageError{message: "--replicas must be a nonnegative int32"}
		}
		count := int32(value)
		input.Replicas = &count
	}
	if err := input.Validate(endpoint.kind, action); err != nil {
		return usageError{message: err.Error()}
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	preview, err := client.ExecuteKubernetesWorkloadAction(ctx, *cluster, *namespace, endpoint.kind, positionals[1], action, input)
	if err != nil {
		return err
	}
	if *dryRun {
		return writeComputeResult(rt.Out, format, preview, nil)
	}
	if err := writeStructuredOutput(rt.Err, format, preview); err != nil {
		return err
	}
	if !*yes {
		confirmed, err := confirmAction(rt, fmt.Sprintf("%s %s/%s in cluster %s namespace %s (UID %s, version %s)?", action, endpoint.kind, terminalLabel(positionals[1]), terminalLabel(*cluster), terminalLabel(*namespace), terminalLabel(*uid), terminalLabel(*version)))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("workload action declined; pass --yes to confirm non-interactively")
		}
	}
	input.DryRun = false
	item, err := client.ExecuteKubernetesWorkloadAction(ctx, *cluster, *namespace, endpoint.kind, positionals[1], action, input)
	return writeComputeResult(rt.Out, format, item, err)
}
