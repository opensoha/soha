package sohacli

import (
	"context"
	"net/http"
	"net/url"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runKubernetesRollout(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 || args[0] != "status" && args[0] != "history" {
		return usageError{message: "k8s rollout requires status or history"}
	}
	leading, rest := extractLeadingPositionals(args[1:], 2)
	fs := newRuntimeFlagSet("k8s rollout "+args[0], rest, rt)
	profile := fs.String("profile", "", "profile name")
	cluster := fs.String("cluster-id", "", "Soha cluster ID")
	namespace := fs.String("namespace", "default", "one Kubernetes namespace")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 2 {
		return usageError{message: "provide Deployment and one workload name"}
	}
	endpoint, err := kubernetesEndpoint(positionals[0])
	if err != nil {
		return err
	}
	if endpoint.kind != "Deployment" {
		return usageError{message: "the server rollout status/history API supports Deployment"}
	}
	if !kubernetesSegment(positionals[1]) || !kubernetesSegment(*namespace) {
		return usageError{message: "one valid name and namespace are required"}
	}
	base, err := kubernetesClusterPath(*cluster)
	if err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	suffix := "rollout-status"
	var out any = &sohaapi.KubernetesDeploymentRolloutStatusEnvelope{}
	if args[0] == "history" {
		suffix = "rollouts"
		out = &sohaapi.KubernetesDeploymentRolloutListEnvelope{}
	}
	path := base + "/workloads/deployments/" + url.PathEscape(positionals[1]) + "/" + suffix
	err = client.doJSON(ctx, http.MethodGet, kubernetesQuery(path, *namespace), client.Token, nil, nil, out)
	return writeComputeResult(rt.Out, format, out, err)
}
