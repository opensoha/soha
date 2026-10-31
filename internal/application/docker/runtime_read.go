package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type RuntimeReadInput struct {
	Action       string
	ServiceName  string
	Target       string
	Path         string
	TailLines    int
	SinceSeconds int64
	Limit        int
	LimitBytes   int64
}

type boundReadTarget struct {
	project domain.Project
	host    domain.Host
	service domain.Service
	binding protocol.ContainerBinding
	token   string
}

var boundRuntimeReadClient = &http.Client{Transport: dockerRuntimeHTTPTransport, Timeout: 65 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (s *Service) getBoundProjectLogs(ctx context.Context, principal domainidentity.Principal, id, name string, tail int, since int64) (domain.ProjectRuntimeLogs, error) {
	result, err := s.ReadProjectRuntime(ctx, principal, id, RuntimeReadInput{Action: "logs", ServiceName: name, TailLines: tail, SinceSeconds: since})
	if err != nil {
		return domain.ProjectRuntimeLogs{}, err
	}
	return domain.ProjectRuntimeLogs{ProjectID: result.ProjectID, ServiceName: result.Container.ServiceName, ContainerID: result.Container.ContainerID, TailLines: normalizeRuntimeTailLines(tail), Content: result.Content, Source: "agent_docker_runtime_v1", Truncated: result.Truncated}, nil
}

func (s *Service) ReadProjectRuntime(ctx context.Context, principal domainidentity.Principal, projectID string, input RuntimeReadInput) (_ protocol.ReadResult, retErr error) {
	permission, err := runtimeReadPermission(input.Action)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	defer func() {
		result, summary := "success", "completed bounded identity-bound Docker read"
		if retErr != nil {
			result, summary = "failure", "bounded identity-bound Docker read failed"
		}
		s.recordLogAudit(ctx, principal, projectID, input.ServiceName, "docker.runtime."+input.Action, result, summary)
	}()
	if err := s.authorize(ctx, principal, permission); err != nil {
		return protocol.ReadResult{}, err
	}
	target, err := s.boundReadTarget(ctx, projectID, input.ServiceName)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	req, err := (protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: input.Action, ProjectID: target.project.ID, Container: target.binding,
		Target: input.Target, Path: input.Path, TailLines: input.TailLines, SinceSeconds: input.SinceSeconds, Limit: input.Limit, LimitBytes: input.LimitBytes}).Normalized(input.Action)
	if err != nil {
		return protocol.ReadResult{}, fmt.Errorf("%w: invalid Docker runtime read options", apperrors.ErrInvalidArgument)
	}
	result, err := postBoundRuntimeRead(ctx, target, req)
	if err != nil {
		return protocol.ReadResult{}, err
	}
	if err := s.authorize(ctx, principal, permission); err != nil {
		return protocol.ReadResult{}, err
	}
	if err := s.confirmBoundReadTarget(ctx, target); err != nil {
		return protocol.ReadResult{}, err
	}
	return result, nil
}

func runtimeReadPermission(action string) (string, error) {
	switch action {
	case "logs":
		return appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "logs"), nil
	case "volumes", "files.list", "files.read":
		return appaccess.PermDockerServicesFiles, nil
	default:
		return "", fmt.Errorf("%w: unsupported Docker runtime read action", apperrors.ErrInvalidArgument)
	}
}

func (s *Service) boundReadTarget(ctx context.Context, id, serviceName string) (boundReadTarget, error) {
	var target boundReadTarget
	if err := validateRuntimeReadTargetInput(id, serviceName); err != nil {
		return target, err
	}
	project, err := s.repo.GetProject(ctx, id)
	if err != nil {
		return target, err
	}
	if err := checkProjectScope(ctx, project); err != nil {
		return target, err
	}
	if project.RuntimeIdentityVersion != 1 {
		return target, fmt.Errorf("%w: Docker runtime read requires a bound project", apperrors.ErrUnsupportedOperation)
	}
	host, err := s.repo.GetHost(ctx, project.HostID)
	if err != nil {
		return target, err
	}
	binding, err := protocol.NewProjectBinding(host.ResourceUID, project.ResourceUID)
	if err != nil || host.ID != project.HostID {
		return target, apperrors.ErrConflict
	}
	service, err := s.boundReadService(ctx, project, serviceName)
	if err != nil {
		return target, err
	}
	container := protocol.ContainerBinding{ProjectBinding: binding, ServiceName: service.Name, ContainerID: service.ContainerID}
	if container.Validate() != nil {
		return target, fmt.Errorf("%w: registered container identity is unavailable", apperrors.ErrConflict)
	}
	if err := validateBoundReadEndpoint(host.Endpoint); err != nil {
		return target, err
	}
	token, err := s.hostAgentBearerToken(ctx, host.ID)
	if err != nil {
		return target, err
	}
	return boundReadTarget{project: project, host: host, service: service, binding: container, token: token}, nil
}

func validateRuntimeReadTargetInput(id, name string) error {
	if !protocol.SafeText(id, 128) || id == "" || id != strings.TrimSpace(id) || !protocol.SafeText(name, 128) || name != strings.TrimSpace(name) {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func validateBoundReadEndpoint(value string) error {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("%w: Docker runtime endpoint is unavailable", apperrors.ErrClusterUnready)
	}
	return nil
}

func (s *Service) boundReadService(ctx context.Context, project domain.Project, name string) (domain.Service, error) {
	items, err := s.repo.ListServices(ctx, domain.ServiceFilter{HostID: project.HostID, ProjectID: project.ID, Limit: 257})
	if err != nil {
		return domain.Service{}, err
	}
	if len(items) > 256 {
		return domain.Service{}, apperrors.ErrUnsupportedOperation
	}
	var selected domain.Service
	count := 0
	for _, item := range items {
		if name == "" || item.Name == name {
			selected = item
			count++
		}
	}
	if count == 0 {
		return domain.Service{}, apperrors.ErrNotFound
	}
	if count != 1 {
		if name == "" {
			return domain.Service{}, fmt.Errorf("%w: serviceName is required for a multi-service project", apperrors.ErrInvalidArgument)
		}
		return domain.Service{}, apperrors.ErrConflict
	}
	if err := checkServiceProject(ctx, selected, project); err != nil {
		return domain.Service{}, err
	}
	return selected, nil
}

func (s *Service) confirmBoundReadTarget(ctx context.Context, old boundReadTarget) error {
	project, err := s.repo.GetProject(ctx, old.project.ID)
	if err != nil || project.ResourceUID != old.project.ResourceUID || project.ConfigurationRevision != old.project.ConfigurationRevision || project.HostID != old.host.ID || project.RuntimeIdentityVersion != 1 {
		return apperrors.ErrConflict
	}
	host, err := s.repo.GetHost(ctx, old.host.ID)
	if err != nil || host.ResourceUID != old.host.ResourceUID || host.ConfigurationRevision != old.host.ConfigurationRevision || host.Endpoint != old.host.Endpoint {
		return apperrors.ErrConflict
	}
	service, err := s.repo.GetService(ctx, old.service.ID)
	if err != nil || service.Name != old.service.Name || service.ContainerID != old.binding.ContainerID {
		return apperrors.ErrConflict
	}
	return checkServiceProject(ctx, service, project)
}

func postBoundRuntimeRead(ctx context.Context, target boundReadTarget, req protocol.ReadRequest) (protocol.ReadResult, error) {
	var zero protocol.ReadResult
	path := map[string]string{"logs": "logs", "volumes": "volumes", "files.list": "files/list", "files.read": "files/read"}[req.Action]
	if path == "" {
		return zero, apperrors.ErrInvalidArgument
	}
	body, err := json.Marshal(req)
	if err != nil {
		return zero, apperrors.ErrInvalidArgument
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, dockerRuntimeURL(target.host.Endpoint, "/docker/runtime/v1/"+path), bytes.NewReader(body))
	if err != nil {
		return zero, apperrors.ErrClusterUnready
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+target.token)
	response, err := boundRuntimeReadClient.Do(request)
	if err != nil {
		return zero, fmt.Errorf("%w: Docker versioned runtime read is unavailable", apperrors.ErrClusterUnready)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented || response.StatusCode == http.StatusForbidden {
		return zero, fmt.Errorf("%w: Docker Agent versioned read capability is unavailable", apperrors.ErrUnsupportedOperation)
	}
	if response.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("%w: Docker versioned runtime read could not be confirmed", apperrors.ErrClusterUnready)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 || !utf8.Valid(raw) {
		return zero, apperrors.ErrClusterUnready
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope dockerRuntimeItemResponse[protocol.ReadResult]
	if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF || envelope.Data.ValidateFor(req) != nil {
		return zero, fmt.Errorf("%w: Docker runtime response differs from the original request", apperrors.ErrConflict)
	}
	return envelope.Data, nil
}
