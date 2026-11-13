package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func bindDockerProjectDeployPlan(body any, id string, plan sohaapi.OperationalPlan) error {
	input, ok := body.(*sohaapi.DockerProjectDeployInput)
	if !ok {
		return nil
	}
	value := plan.DockerProjectDeploy
	if !plan.Ready || plan.Target != id || plan.Capability != "docker.projects.deploy.trigger" || value == nil ||
		!kubernetesSegment(value.HostID) || strings.TrimSpace(value.HostID) != value.HostID ||
		value.Host.ExpectedUID == uuid.Nil || value.Host.ExpectedConfigurationRevision == uuid.Nil ||
		value.Project.ExpectedUID == uuid.Nil || value.Project.ExpectedConfigurationRevision == uuid.Nil {
		return fmt.Errorf("server did not return a complete identity-bound Docker deployment plan for this project; no execution request was sent")
	}
	bound := *value
	input.Preconditions = &bound
	return nil
}

func readOriginalDockerDeployment(ctx context.Context, rt Runtime, client APIClient, format string, req dockerWorkbenchRequest, opt dockerWorkbenchOptions) error {
	path := "/api/v1/docker/projects/" + url.PathEscape(req.id) + "/deploy-requests/" + url.PathEscape(opt.key) + "?action=" + url.QueryEscape(opt.intent)
	if err := client.doJSON(ctx, http.MethodGet, path, client.Token, nil, nil, req.out); err != nil {
		return fmt.Errorf("%w; original deployment was not recovered; no plan or execution request was sent", err)
	}
	return writeWorkbenchOutput(rt, client, format, dockerWorkbenchProjection(req.out))
}
