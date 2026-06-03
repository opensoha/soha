package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	appaccess "github.com/soha/soha/internal/application/access"
	domaindocker "github.com/soha/soha/internal/domain/docker"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	"github.com/soha/soha/internal/platform/apperrors"
)

const (
	defaultRuntimeLogTailLines = 200
	maxRuntimeLogTailLines     = 2000
	defaultVolumeListLimit     = 200
	maxVolumeListLimit         = 1000
	defaultVolumeReadLimit     = 256 * 1024
	maxVolumeReadLimit         = 1024 * 1024
)

var runtimeServiceNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type dockerRuntimeRequest struct {
	ProjectID      string         `json:"projectId"`
	ProjectName    string         `json:"projectName"`
	ComposeContent string         `json:"composeContent"`
	EnvContent     string         `json:"envContent,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
	ServiceName    string         `json:"serviceName,omitempty"`
	TailLines      int            `json:"tailLines,omitempty"`
	Target         string         `json:"target,omitempty"`
	Path           string         `json:"path,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	LimitBytes     int64          `json:"limitBytes,omitempty"`
	Shell          string         `json:"shell,omitempty"`
}

type dockerRuntimeItemResponse[T any] struct {
	Data T `json:"data"`
}

type dockerRuntimeMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *Service) GetProjectLogs(ctx context.Context, principal domainidentity.Principal, projectID, serviceName string, tailLines int) (domaindocker.ProjectRuntimeLogs, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return domaindocker.ProjectRuntimeLogs{}, err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, serviceName)
	if err != nil {
		return domaindocker.ProjectRuntimeLogs{}, err
	}
	req := target.runtimeRequest()
	req.TailLines = normalizeRuntimeTailLines(tailLines)
	return postDockerRuntime[domaindocker.ProjectRuntimeLogs](ctx, target.Endpoint, s.runtimeBearerToken, "/docker/runtime/logs", req)
}

func (s *Service) StreamProjectLogs(ctx context.Context, principal domainidentity.Principal, projectID, serviceName string, tailLines int, stdout io.Writer) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, serviceName)
	if err != nil {
		return err
	}
	req := target.runtimeRequest()
	req.TailLines = normalizeRuntimeTailLines(tailLines)
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, dockerRuntimeURL(target.Endpoint, "/docker/runtime/logs/stream"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.runtimeBearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.runtimeBearerToken)
	}
	resp, err := (&http.Client{}).Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: docker agent log stream unavailable: %v", apperrors.ErrClusterUnready, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%w: docker agent log stream returned %d: %s", apperrors.ErrClusterUnready, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	_, err = io.Copy(stdout, resp.Body)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (s *Service) StreamProjectTerminal(ctx context.Context, principal domainidentity.Principal, projectID, serviceName, shell string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesManage); err != nil {
		return err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, serviceName)
	if err != nil {
		return err
	}
	req := target.runtimeRequest()
	req.Shell = shell
	wsURL := dockerRuntimeWebSocketURL(target.Endpoint, "/docker/runtime/terminal")
	headers := http.Header{}
	if s.runtimeBearerToken != "" {
		headers.Set("Authorization", "Bearer "+s.runtimeBearerToken)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("%w: docker agent terminal unavailable: %v", apperrors.ErrClusterUnready, err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(dockerRuntimeMessage{Type: "init", Data: mustJSON(req)}); err != nil {
		return err
	}
	var writeMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			n, readErr := stdin.Read(buffer)
			if n > 0 {
				writeMu.Lock()
				_ = conn.WriteJSON(dockerRuntimeMessage{Type: "input", Data: string(buffer[:n])})
				writeMu.Unlock()
			}
			if readErr != nil {
				writeMu.Lock()
				_ = conn.WriteJSON(dockerRuntimeMessage{Type: "close"})
				writeMu.Unlock()
				return
			}
		}
	}()
	for {
		var message dockerRuntimeMessage
		if err := conn.ReadJSON(&message); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		switch message.Type {
		case "stdout":
			_, _ = io.WriteString(stdout, message.Data)
		case "stderr":
			_, _ = io.WriteString(stderr, message.Data)
		case "exit":
			return nil
		case "error":
			return fmt.Errorf("%w: %s", apperrors.ErrClusterUnready, message.Message)
		}
	}
}

func (s *Service) ListProjectVolumes(ctx context.Context, principal domainidentity.Principal, projectID, serviceName string) ([]domaindocker.ProjectVolume, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return nil, err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, serviceName)
	if err != nil {
		return nil, err
	}
	return postDockerRuntime[[]domaindocker.ProjectVolume](ctx, target.Endpoint, s.runtimeBearerToken, "/docker/runtime/volumes", target.runtimeRequest())
}

func (s *Service) ListProjectVolumeFiles(ctx context.Context, principal domainidentity.Principal, projectID string, input domaindocker.ProjectVolumeFileListInput) (domaindocker.ProjectVolumeFileList, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return domaindocker.ProjectVolumeFileList{}, err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, input.ServiceName)
	if err != nil {
		return domaindocker.ProjectVolumeFileList{}, err
	}
	req := target.runtimeRequest()
	req.Target = input.Target
	req.Path = input.Path
	req.Limit = normalizeVolumeListLimit(input.Limit)
	return postDockerRuntime[domaindocker.ProjectVolumeFileList](ctx, target.Endpoint, s.runtimeBearerToken, "/docker/runtime/volume-files", req)
}

func (s *Service) ReadProjectVolumeFile(ctx context.Context, principal domainidentity.Principal, projectID string, input domaindocker.ProjectVolumeFileReadInput) (domaindocker.ProjectVolumeFileContent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return domaindocker.ProjectVolumeFileContent{}, err
	}
	target, err := s.projectRuntimeTarget(ctx, projectID, input.ServiceName)
	if err != nil {
		return domaindocker.ProjectVolumeFileContent{}, err
	}
	req := target.runtimeRequest()
	req.Target = input.Target
	req.Path = input.Path
	req.LimitBytes = normalizeVolumeReadLimit(input.LimitBytes)
	return postDockerRuntime[domaindocker.ProjectVolumeFileContent](ctx, target.Endpoint, s.runtimeBearerToken, "/docker/runtime/volume-file", req)
}

type dockerRuntimeTarget struct {
	Project     domaindocker.Project
	ServiceName string
	Endpoint    string
}

func (t dockerRuntimeTarget) runtimeRequest() dockerRuntimeRequest {
	return dockerRuntimeRequest{
		ProjectID:      t.Project.ID,
		ProjectName:    runtimeComposeProjectName(t.Project),
		ComposeContent: t.Project.ComposeContent,
		EnvContent:     t.Project.EnvContent,
		Config:         t.Project.Config,
		ServiceName:    t.ServiceName,
	}
}

func (s *Service) projectRuntimeTarget(ctx context.Context, projectID, requestedServiceName string) (dockerRuntimeTarget, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return dockerRuntimeTarget{}, fmt.Errorf("%w: project id is required", apperrors.ErrInvalidArgument)
	}
	project, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return dockerRuntimeTarget{}, err
	}
	if strings.TrimSpace(project.ComposeContent) == "" {
		return dockerRuntimeTarget{}, fmt.Errorf("%w: composeContent is required for docker runtime access", apperrors.ErrInvalidArgument)
	}
	host, err := s.repo.GetHost(ctx, project.HostID)
	if err != nil {
		return dockerRuntimeTarget{}, err
	}
	endpoint := strings.TrimSpace(host.Endpoint)
	if endpoint == "" {
		return dockerRuntimeTarget{}, fmt.Errorf("%w: docker host agent endpoint is not available", apperrors.ErrClusterUnready)
	}
	serviceName := strings.TrimSpace(requestedServiceName)
	if serviceName == "" {
		serviceName = stringValue(project.Config, "serviceName")
	}
	services := composeServiceNames(project.ComposeContent)
	if serviceName == "" && len(services) == 1 {
		serviceName = services[0]
	}
	if serviceName == "" {
		return dockerRuntimeTarget{}, fmt.Errorf("%w: serviceName is required for multi-service compose projects", apperrors.ErrInvalidArgument)
	}
	if !runtimeServiceNamePattern.MatchString(serviceName) {
		return dockerRuntimeTarget{}, fmt.Errorf("%w: invalid docker service name", apperrors.ErrInvalidArgument)
	}
	if len(services) > 0 && !slices.Contains(services, serviceName) {
		return dockerRuntimeTarget{}, fmt.Errorf("%w: docker service %s is not defined in compose", apperrors.ErrNotFound, serviceName)
	}
	return dockerRuntimeTarget{Project: project, ServiceName: serviceName, Endpoint: endpoint}, nil
}

func postDockerRuntime[T any](ctx context.Context, endpoint string, token string, runtimePath string, payload dockerRuntimeRequest) (T, error) {
	var zero T
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dockerRuntimeURL(endpoint, runtimePath), bytes.NewReader(body))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return zero, fmt.Errorf("%w: docker agent runtime unavailable: %v", apperrors.ErrClusterUnready, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return zero, fmt.Errorf("%w: docker agent runtime returned %d: %s", apperrors.ErrClusterUnready, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	var out dockerRuntimeItemResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("%w: decode docker agent runtime response: %v", apperrors.ErrClusterUnready, err)
	}
	return out.Data, nil
}

func dockerRuntimeURL(endpoint string, runtimePath string) string {
	base, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return strings.TrimRight(endpoint, "/") + "/api/v1" + runtimePath
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/v1" + runtimePath
	return base.String()
}

func dockerRuntimeWebSocketURL(endpoint string, runtimePath string) string {
	raw := dockerRuntimeURL(endpoint, runtimePath)
	if strings.HasPrefix(raw, "https://") {
		return "wss://" + strings.TrimPrefix(raw, "https://")
	}
	return "ws://" + strings.TrimPrefix(raw, "http://")
}

func runtimeComposeProjectName(project domaindocker.Project) string {
	name := NormalizeSlug(firstNonEmpty(project.Slug, project.ID, project.Name))
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.Trim(name, "-_")
	if name == "" {
		return "soha-docker"
	}
	return name
}

func normalizeRuntimeTailLines(value int) int {
	if value <= 0 {
		return defaultRuntimeLogTailLines
	}
	if value > maxRuntimeLogTailLines {
		return maxRuntimeLogTailLines
	}
	return value
}

func normalizeVolumeListLimit(value int) int {
	if value <= 0 {
		return defaultVolumeListLimit
	}
	if value > maxVolumeListLimit {
		return maxVolumeListLimit
	}
	return value
}

func normalizeVolumeReadLimit(value int64) int64 {
	if value <= 0 {
		return defaultVolumeReadLimit
	}
	if value > maxVolumeReadLimit {
		return maxVolumeReadLimit
	}
	return value
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
