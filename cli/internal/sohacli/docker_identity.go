package sohacli

import (
	"context"
	"fmt"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"net/url"
)

func runDockerIdentity(ctx context.Context, resource string, args []string, rt Runtime) error {
	if resource != "hosts" && resource != "projects" {
		return usageError{message: "Docker identity supports hosts and projects"}
	}
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("docker "+resource+" identity", rest, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 || !kubernetesSegment(ids[0]) {
		return usageError{message: "Docker identity requires one valid resource ID"}
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	req := dockerWorkbenchRequest{path: "/api/v1/docker/" + resource + "/" + url.PathEscape(ids[0]) + "/identity", id: ids[0], out: &sohaapi.DockerResourceIdentityEnvelope{}}
	return executeDockerWorkbench(ctx, rt, client, format, resource, "identity", req, dockerWorkbenchOptions{})
}

func validateDockerIdentityResponse(req dockerWorkbenchRequest, resource string) error {
	envelope, ok := req.out.(*sohaapi.DockerResourceIdentityEnvelope)
	if !ok {
		return fmt.Errorf("invalid Docker identity response")
	}
	item := envelope.Data
	kind := "docker.host"
	if resource == "projects" {
		kind = "docker.project"
	}
	if string(item.ResourceKind) != kind || item.ID != req.id || item.UID == [16]byte{} || item.ConfigurationRevision == [16]byte{} || resource == "projects" && item.HostID == "" || resource == "hosts" && item.HostID != "" {
		return fmt.Errorf("docker identity response does not match the requested resource or lacks database identity; no identity was synthesized")
	}
	return nil
}
