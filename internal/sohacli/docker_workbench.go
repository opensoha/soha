package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func dockerCommandSpec() commandSpec {
	return commandSpec{Name: "docker", Usage: "soha docker <hosts|projects|services|tasks|logs|volumes|files|configuration-requests> <action> [options]", Summary: "Operate Docker through Soha connections, permissions and durable tasks", Handler: runDocker, Examples: []string{"soha docker projects plan project-1 --profile local"}, Subcommands: []commandSpec{
		{Name: "hosts", Usage: "soha docker hosts <list|get|identity|register|update|delete|plan|create> [HOST] [options]", Summary: "List safe host metadata, read a reference or identity, or preflight and provision its backing VM", CompletionWords: []string{"list", "get", "identity", "register", "update", "delete", "plan", "create"}, Examples: []string{"soha docker hosts list --search worker --page-size 20 --profile local", "soha docker hosts identity host-1 --profile local", "soha docker hosts plan --input host.json --profile local", "soha docker hosts create --input host.json --idempotency-key host-create-1 --yes --profile local"}},
		{Name: "projects", Usage: "soha docker projects <list|get|identity|create|update|delete|plan|deploy|terminal|terminal-session|terminal-close> [PROJECT] [options]", Summary: "Read safe project inventory or identity, save inline Compose or submit a planned runtime action", CompletionWords: []string{"list", "get", "identity", "create", "update", "delete", "plan", "deploy", "terminal", "terminal-session", "terminal-close"}, Examples: []string{"soha docker projects terminal project-1 --service api --expected-container-id CID --yes --profile local", "soha docker projects list --host-id host-1 --profile local", "soha docker projects identity project-1 --profile local", "soha docker projects create --input project.json --idempotency-key project-create-1 --yes --profile local", "soha docker projects plan project-1 --action deploy --profile local", "soha docker projects deploy project-1 --action restart --idempotency-key restart-1 --yes --profile local"}},
		{Name: "services", Usage: "soha docker services <list|get|action> [SERVICE] [ACTION] [options]", Summary: "List safe service inventory, read a reference or submit a typed start/stop/restart action", CompletionWords: []string{"list", "get", "action", "start", "stop", "restart"}, Examples: []string{"soha docker services list --project-id project-1 --profile local", "soha docker services get service-1 --profile local", "soha docker services action service-1 restart --idempotency-key restart-1 --yes --profile local"}},
		{Name: "tasks", Usage: "soha docker tasks <list|get|logs|cancel|retry|wait> [TASK] [options]", Summary: "Inspect and control container_runtime tasks; wait timeout only detaches", CompletionWords: []string{"list", "get", "logs", "cancel", "retry", "wait"}, Examples: []string{"soha docker tasks list --profile local", "soha docker tasks wait task-1 --wait-timeout 10m --profile local"}},
		{Name: "configuration-requests", Usage: "soha docker configuration-requests get KEY [options]", Summary: "Read the original actor-scoped conditional metadata receipt, including deletion tombstones", CompletionWords: []string{"get"}, Examples: []string{"soha docker configuration-requests get original-key --profile local"}},
		{Name: "logs", Usage: "soha docker logs <get|query|tail> --project-id ID [options]", Summary: "Read identity-bound logs, query normalized entries or follow with bounded polling", CompletionWords: []string{"get", "query", "tail"}, Examples: []string{"soha docker logs get --project-id project-1 --service api --tail-lines 100 --profile local", "soha docker logs query --project-id project-1 --service api --profile local", "soha docker logs tail --project-id project-1 --service api --profile local"}},
		{Name: "volumes", Usage: "soha docker volumes list --project-id ID --service NAME [options]", Summary: "Inspect actual identity-bound mount targets and safe-reader capability without host source paths", CompletionWords: []string{"list"}, Examples: []string{"soha docker volumes list --project-id project-1 --service api --profile local"}},
		{Name: "files", Usage: "soha docker files <list|read> --project-id ID --service NAME --target PATH [options]", Summary: "Read bounded UTF-8 mount content with explicit identity and completeness; no Compose fallback", CompletionWords: []string{"list", "read"}, Examples: []string{"soha docker files list --project-id project-1 --service api --target /data --path logs --profile local", "soha docker files read --project-id project-1 --service api --target /data --path logs/app.log --limit-bytes 65536 --profile local"}},
	}}
}

func runDocker(ctx context.Context, args []string, rt Runtime) error {
	if len(args) < 2 {
		return usageError{message: "docker requires hosts, projects, services, tasks or logs and an action"}
	}
	resource, action, rest := args[0], args[1], args[2:]
	if resource == "projects" && (action == "terminal" || action == "terminal-session" || action == "terminal-close") {
		return runDockerNativeTerminal(ctx, append([]string{action}, rest...), rt)
	}
	if resource == "files" || resource == "volumes" || resource == "logs" && action == "get" {
		return runDockerRuntimeRead(ctx, resource, action, rest, rt)
	}
	if resource == "configuration-requests" || (resource == "hosts" || resource == "projects") && (action == "register" || action == "update" || action == "delete") {
		return runDockerConfiguration(ctx, resource, action, rest, rt)
	}
	if err := validateDockerAliasedFlags(resource, rest); err != nil {
		return err
	}
	if action == "identity" {
		return runDockerIdentity(ctx, resource, rest, rt)
	}
	if action == "list" && (resource == "hosts" || resource == "projects" || resource == "services") {
		return runDockerInventory(ctx, resource, rest, rt)
	}
	switch resource {
	case "logs":
		return runLogs(ctx, append([]string{action, "--source", "docker"}, rest...), rt)
	case "tasks":
		if action == "list" {
			return runComputeTasks(ctx, append([]string{action, "--domain", "container_runtime"}, rest...), rt)
		}
		leading, flags := extractLeadingPositionals(rest, 1)
		return runComputeTasks(ctx, append(append([]string{action, "container_runtime"}, leading...), flags...), rt)
	case "services", "hosts":
		if action == "get" || resource == "services" && action == "action" {
			kind := "service"
			if resource == "hosts" {
				kind = "runtime_host"
			}
			return runComputeResources(ctx, append([]string{action, "container_runtime", kind}, rest...), rt)
		}
	}
	return runDockerWorkbench(ctx, resource, action, rest, rt)
}

func validateDockerAliasedFlags(resource string, args []string) error {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if resource == "tasks" && name == "--domain" || resource == "logs" && name == "--source" {
			return usageError{message: "the Docker command fixes its source/domain; use the corresponding workbench for another domain"}
		}
		if resource == "services" && (name == "--reason" || name == "--input-json") {
			return usageError{message: "Docker service actions do not support reason or metadata in the current server contract"}
		}
	}
	return nil
}

type dockerWorkbenchRequest struct {
	path, preflightPath, id string
	body, out               any
	write                   bool
}

type dockerWorkbenchOptions struct {
	profile, output, file, inline, key, intent string
	yes, retry                                 bool
}

func runDockerWorkbench(ctx context.Context, resource, action string, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("docker "+resource+" "+action, rest, rt)
	var opt dockerWorkbenchOptions
	fs.StringVar(&opt.profile, "profile", "", "profile name")
	fs.StringVar(&opt.output, "output", "json", "output format: json or yaml")
	fs.StringVar(&opt.file, "input", "", "typed JSON file, or - for stdin")
	fs.StringVar(&opt.inline, "input-json", "", "typed JSON request")
	fs.StringVar(&opt.key, "idempotency-key", "", "stable original request key; required for mutations")
	fs.StringVar(&opt.intent, "action", "deploy", "project action: deploy, redeploy, start, stop, restart, down, pull, build or destroy")
	fs.BoolVar(&opt.yes, "yes", false, "confirm the exact target; server authorization and approval still apply")
	fs.BoolVar(&opt.retry, "retry-original", false, "recover the original request; project deployment uses a read-only receipt lookup")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	req, allowed, err := prepareDockerWorkbench(resource, action, append(leading, fs.Args()...), opt.intent)
	if err != nil {
		return err
	}
	if req.write {
		allowed = append(allowed, "idempotency-key", "yes")
		if len(opt.key) < 8 || len(opt.key) > 128 || !kubernetesSegment(opt.key) {
			return usageError{message: "provide a stable --idempotency-key of 8..128 characters without whitespace or controls"}
		}
	}
	if req.preflightPath != "" {
		allowed = append(allowed, "retry-original")
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return err
	}
	if resource == "hosts" || resource == "projects" && action == "create" {
		if err := readTypedWorkbenchInput(rt, opt.file, opt.inline, req.body); err != nil {
			return err
		}
		if err := validateDockerWorkbenchInput(req.body, opt.key); err != nil {
			return err
		}
	}
	format, client, err := computeOutputClient(ctx, rt, opt.profile, opt.output)
	if err != nil {
		return err
	}
	return executeDockerWorkbench(ctx, rt, client, format, resource, action, req, opt)
}

func executeDockerWorkbench(ctx context.Context, rt Runtime, client APIClient, format, resource, action string, req dockerWorkbenchRequest, opt dockerWorkbenchOptions) error {
	// A redirect must never replay a mutation to another or older endpoint.
	client.Client = client.httpClient()
	client.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if resource == "projects" && action == "deploy" && opt.retry {
		return readOriginalDockerDeployment(ctx, rt, client, format, req, opt)
	}
	if req.preflightPath != "" && !opt.retry {
		var preview sohaapi.OperationalPlanEnvelope
		if err := client.doJSON(ctx, http.MethodPost, req.preflightPath, client.Token, nil, req.body, &preview); err != nil {
			return err
		}
		if err := writeStructuredOutput(rt.Err, format, sanitizeCLIJSONValue(preview)); err != nil {
			return err
		}
		if !preview.Data.Ready {
			return fmt.Errorf("docker plan is not ready; no execution request was sent")
		}
		if err := bindDockerProjectDeployPlan(req.body, req.id, preview.Data); err != nil {
			return err
		}
	}
	if req.write {
		if err := confirmWorkbenchAction(rt, opt.yes, dockerWorkbenchConfirmation(resource, action, opt.intent, req)); err != nil {
			return err
		}
	}
	method := http.MethodPost
	if action == "get" || action == "identity" {
		method = http.MethodGet
	}
	if err := client.doJSON(ctx, method, req.path, client.Token, idempotencyHeaders(opt.key), req.body, req.out); err != nil {
		if req.write {
			return fmt.Errorf("%w; inspect the original Docker task/project receipt before retrying the same input and key; no automatic replay", err)
		}
		return err
	}
	if action == "identity" {
		if err := validateDockerIdentityResponse(req, resource); err != nil {
			return err
		}
	}
	return writeWorkbenchOutput(rt, client, format, dockerWorkbenchProjection(req.out))
}

func dockerWorkbenchConfirmation(resource, action, intent string, req dockerWorkbenchRequest) string {
	switch value := req.body.(type) {
	case *sohaapi.DockerProjectInput:
		return "Save Docker project configuration " + terminalLabel(value.Name) + " on host " + terminalLabel(value.HostID) + " using the reviewed input?"
	case *sohaapi.DockerQuickCreateHostInput:
		return "Provision Docker host " + terminalLabel(value.Name) + " and its backing VM using the reviewed input?"
	default:
		return "Run Docker " + terminalLabel(resource) + " " + terminalLabel(action) + " " + terminalLabel(req.id) + " (" + terminalLabel(intent) + ") using the reviewed input?"
	}
}

func prepareDockerWorkbench(resource, action string, ids []string, intent string) (dockerWorkbenchRequest, []string, error) {
	req := dockerWorkbenchRequest{path: "/api/v1/docker"}
	allowed := []string{}
	switch {
	case resource == "hosts" && (action == "plan" || action == "create"):
		req.path += "/hosts/quick-create"
		req.body = &sohaapi.DockerQuickCreateHostInput{}
		allowed = append(allowed, "input", "input-json")
		if action == "plan" {
			req.path += "/plan"
			req.out = &sohaapi.OperationalPlanEnvelope{}
		} else {
			req.write, req.preflightPath, req.out = true, req.path+"/plan", &sohaapi.DockerOperationEnvelope{}
		}
	case resource == "projects" && action == "create":
		req.path += "/projects"
		req.body, req.out, req.write = &sohaapi.DockerProjectInput{}, &sohaapi.DockerProjectEnvelope{}, true
		allowed = append(allowed, "input", "input-json")
	case resource == "projects" && (action == "get" || action == "plan" || action == "deploy"):
		if len(ids) != 1 || !kubernetesSegment(ids[0]) {
			return req, nil, usageError{message: "this Docker project action requires one valid Soha project ID"}
		}
		req.id, req.path = ids[0], req.path+"/projects/"+url.PathEscape(ids[0])
		if action == "get" {
			req.out = &sohaapi.DockerProjectEnvelope{}
		} else {
			input := &sohaapi.DockerProjectDeployInput{Action: sohaapi.DockerProjectDeployInputAction(intent)}
			if !input.Action.Valid() {
				return req, nil, usageError{message: "unsupported Docker project runtime action"}
			}
			allowed = append(allowed, "action")
			req.path += "/deploy"
			req.body = input
			if action == "plan" {
				req.path += "/plan"
				req.out = &sohaapi.OperationalPlanEnvelope{}
			} else {
				req.write, req.preflightPath, req.out = true, req.path+"/plan", &sohaapi.DockerOperationEnvelope{}
			}
		}
		return req, allowed, nil
	default:
		return req, nil, usageError{message: "unsupported Docker workbench action"}
	}
	if len(ids) != 0 {
		return req, nil, usageError{message: "this Docker collection action does not accept a resource ID"}
	}
	return req, allowed, nil
}

func validateDockerWorkbenchInput(input any, key string) error {
	switch value := input.(type) {
	case *sohaapi.DockerQuickCreateHostInput:
		if strings.TrimSpace(value.Name) == "" {
			return usageError{message: "Docker host name is required"}
		}
	case *sohaapi.DockerProjectInput:
		if value.ID != "" || value.SourceRef != "" || value.SourceKind != "" && value.SourceKind != "compose" && value.SourceKind != "single_container" || strings.TrimSpace(value.HostID) == "" || strings.TrimSpace(value.Name) == "" || strings.TrimSpace(value.ComposeContent) == "" || value.IdempotencyKey != "" && value.IdempotencyKey != key {
			return usageError{message: "idempotent project creation requires hostId, name and inline Compose, no caller ID/remote source, and a matching original key"}
		}
		value.IdempotencyKey = key
	}
	return nil
}

// User commands never return runner payloads or raw Compose/environment data.
func dockerWorkbenchProjection(value any) any {
	switch envelope := value.(type) {
	case *sohaapi.DockerResourceIdentityEnvelope:
		item := envelope.Data
		data := map[string]any{"resourceKind": item.ResourceKind, "id": item.ID, "uid": item.UID.String(), "configurationRevision": item.ConfigurationRevision.String()}
		if item.ResourceKind == "docker.project" {
			data["hostId"] = item.HostID
		}
		return map[string]any{"data": data}
	case *sohaapi.DockerProjectEnvelope:
		project := envelope.Data
		return map[string]any{"data": map[string]any{"id": project.ID, "hostId": project.HostID, "name": project.Name, "slug": project.Slug, "status": project.Status, "sourceKind": project.SourceKind, "createdAt": project.CreatedAt, "updatedAt": project.UpdatedAt, "configurationHidden": true}}
	case *sohaapi.DockerOperationEnvelope:
		operation := envelope.Data
		return map[string]any{"data": map[string]any{"id": operation.ID, "hostId": operation.HostID, "projectId": operation.ProjectID, "serviceId": operation.ServiceID, "operationKind": operation.OperationKind, "status": operation.Status, "payloadHidden": true}}
	default:
		return value
	}
}
