package gitlab

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var _ domainbuild.PipelineProvider = (*Client)(nil)

type pipelineResponse struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	SHA       string `json:"sha"`
	Ref       string `json:"ref"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	WebURL    string `json:"web_url"`
}

func (p pipelineResponse) run() sohaapi.ExternalPipelineRun {
	return sohaapi.ExternalPipelineRun{RunID: strconv.FormatInt(p.ID, 10), PipelineCommit: p.SHA, Status: p.Status, URL: p.WebURL}
}

func pipelineProjectPath(project string) string { return "/projects/" + url.PathEscape(project) }

func (c *Client) ResolvePipelineTag(ctx context.Context, project, tag string) (string, error) {
	data, err := c.readMetadata(ctx, pipelineProjectPath(project)+"/repository/tags/"+url.PathEscape(tag), nil, 256<<10)
	if err != nil {
		return "", err
	}
	var result branchResponse
	if json.Unmarshal(data, &result) != nil || !result.Protected || result.Name != tag || len(result.Commit.ID) != 40 {
		return "", fmt.Errorf("%w: CI definition requires an existing protected tag", apperrors.ErrInvalidArgument)
	}
	if _, err := hex.DecodeString(result.Commit.ID); err != nil {
		return "", fmt.Errorf("%w: invalid CI definition commit", apperrors.ErrInvalidArgument)
	}
	return result.Commit.ID, nil
}

func (c *Client) StartPipeline(ctx context.Context, request domainbuild.PipelineRequest) (sohaapi.ExternalPipelineRun, error) {
	commit, err := c.ResolvePipelineTag(ctx, request.Spec.ProviderProjectID, request.Spec.Configuration.PipelineTag)
	if err != nil || commit != request.Spec.PipelineCommit {
		return sohaapi.ExternalPipelineRun{Status: "definition_changed", StopConfirmed: true}, fmt.Errorf("%w: frozen CI definition is unavailable or changed", apperrors.ErrConflict)
	}
	variables := make([]map[string]string, 0, len(request.Variables)+4)
	for key, value := range pipelineVariables(request) {
		variables = append(variables, map[string]string{"key": key, "value": value, "variable_type": "env_var"})
	}
	body, err := json.Marshal(map[string]any{"ref": request.Spec.Configuration.PipelineTag, "variables": variables})
	if err != nil {
		return sohaapi.ExternalPipelineRun{Status: "invalid_request", StopConfirmed: true}, err
	}
	data, status, err := c.writePipeline(ctx, pipelineProjectPath(request.Spec.ProviderProjectID)+"/pipeline", body)
	if err != nil {
		return sohaapi.ExternalPipelineRun{Status: "dispatch_unknown", StopConfirmed: status == 400 || status == 401 || status == 403 || status == 404 || status == 405 || status == 422}, err
	}
	var pipeline pipelineResponse
	if json.Unmarshal(data, &pipeline) != nil || pipeline.ID <= 0 {
		return sohaapi.ExternalPipelineRun{Status: "dispatch_unknown"}, fmt.Errorf("%w: invalid CI dispatch response", apperrors.ErrClusterUnready)
	}
	// Preserve the returned ID even on mismatch so reconciliation can confirm stop.
	run := pipeline.run()
	if err := validatePipelineIdentity(pipeline, request); err != nil {
		run.Status = "identity_mismatch"
		return run, err
	}
	return run, nil
}

func pipelineVariables(request domainbuild.PipelineRequest) map[string]string {
	variables := map[string]string{"SOHA_TASK_ID": request.TaskID, "SOHA_SOURCE_COMMIT": request.Spec.SourceCommit, "SOHA_PIPELINE_COMMIT": request.Spec.PipelineCommit, "SOHA_IMAGE": request.Image}
	for key, value := range request.Variables {
		if key == "SOHA_BUILD_ARGS_JSON" || key == "SOHA_VARIABLES_JSON" {
			variables[key] = value
		}
	}
	return variables
}

func validatePipelineIdentity(pipeline pipelineResponse, request domainbuild.PipelineRequest) error {
	if pipeline.ID <= 0 || strconv.FormatInt(pipeline.ProjectID, 10) != request.Spec.ProviderProjectID || pipeline.Ref != request.Spec.Configuration.PipelineTag || pipeline.SHA != request.Spec.PipelineCommit || pipeline.Source != "api" {
		return fmt.Errorf("%w: CI run differs from its frozen identity", apperrors.ErrConflict)
	}
	return nil
}

func (c *Client) writePipeline(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, 0, err
	}
	request, err := c.request(ctx, path, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Method, request.Body, request.ContentLength = http.MethodPost, io.NopCloser(bytes.NewReader(body)), int64(len(body))
	request.Header.Set("Content-Type", "application/json")
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: CI request outcome is unknown", apperrors.ErrClusterUnready)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("%w: CI status %d", apperrors.ErrClusterUnready, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, response.StatusCode, fmt.Errorf("%w: incomplete or oversized CI response", apperrors.ErrClusterUnready)
	}
	return data, response.StatusCode, nil
}

func (c *Client) pipeline(ctx context.Context, project, id string) (pipelineResponse, error) {
	var result pipelineResponse
	data, err := c.readMetadata(ctx, pipelineProjectPath(project)+"/pipelines/"+url.PathEscape(id), nil, 256<<10)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(data, &result) != nil || strconv.FormatInt(result.ID, 10) != id || strconv.FormatInt(result.ProjectID, 10) != project {
		return result, fmt.Errorf("%w: invalid CI run identity", apperrors.ErrConflict)
	}
	return result, nil
}

func (c *Client) pipelineVariablesMatch(ctx context.Context, request domainbuild.PipelineRequest, id string) (bool, error) {
	data, err := c.readMetadata(ctx, pipelineProjectPath(request.Spec.ProviderProjectID)+"/pipelines/"+url.PathEscape(id)+"/variables", nil, 256<<10)
	if err != nil {
		return false, err
	}
	var values []struct{ Key, Value string }
	if json.Unmarshal(data, &values) != nil {
		return false, fmt.Errorf("%w: invalid CI variables", apperrors.ErrClusterUnready)
	}
	expected := pipelineVariables(request)
	matched := map[string]bool{}
	for _, v := range values {
		if want, exists := expected[v.Key]; exists {
			if matched[v.Key] || want != v.Value {
				return false, nil
			}
			matched[v.Key] = true
		}
	}
	return len(matched) == len(expected), nil
}

func (c *Client) FindPipeline(ctx context.Context, request domainbuild.PipelineRequest) (sohaapi.ExternalPipelineRun, error) {
	var matches []pipelineResponse
	params := url.Values{"ref": {request.Spec.Configuration.PipelineTag}, "source": {"api"}, "updated_after": {request.CreatedAt.Add(-time.Hour).UTC().Format(time.RFC3339)}}
	for page := 1; page <= 10; page++ {
		params.Set("page", strconv.Itoa(page))
		params.Set("per_page", "100")
		data, err := c.readMetadata(ctx, pipelineProjectPath(request.Spec.ProviderProjectID)+"/pipelines", params, 1<<20)
		if err != nil {
			return sohaapi.ExternalPipelineRun{}, err
		}
		var pipelines []pipelineResponse
		if json.Unmarshal(data, &pipelines) != nil {
			return sohaapi.ExternalPipelineRun{}, fmt.Errorf("%w: invalid CI run list", apperrors.ErrClusterUnready)
		}
		for _, pipeline := range pipelines {
			if pipeline.Ref != request.Spec.Configuration.PipelineTag || pipeline.ID <= 0 {
				continue
			}
			match, err := c.pipelineVariablesMatch(ctx, request, strconv.FormatInt(pipeline.ID, 10))
			if err != nil {
				return sohaapi.ExternalPipelineRun{}, err
			}
			if match {
				matches = append(matches, pipeline)
			}
		}
		if len(pipelines) < 100 {
			return c.matchedPipeline(ctx, request, matches)
		}
	}
	return sohaapi.ExternalPipelineRun{}, fmt.Errorf("%w: CI reconciliation exceeded its run limit", apperrors.ErrConflict)
}

func (c *Client) matchedPipeline(ctx context.Context, request domainbuild.PipelineRequest, matches []pipelineResponse) (sohaapi.ExternalPipelineRun, error) {
	switch len(matches) {
	case 0:
		return sohaapi.ExternalPipelineRun{Status: "dispatch_unknown"}, nil
	case 1:
		pipeline, err := c.pipeline(ctx, request.Spec.ProviderProjectID, strconv.FormatInt(matches[0].ID, 10))
		if err != nil {
			return sohaapi.ExternalPipelineRun{}, err
		}
		run := pipeline.run()
		if err := validatePipelineIdentity(pipeline, request); err != nil {
			run.Status = "identity_mismatch"
			return run, err
		}
		return run, nil
	default:
		run := sohaapi.ExternalPipelineRun{Status: "duplicate_runs", StopConfirmed: true}
		for _, match := range matches {
			inspection, err := c.InspectPipeline(ctx, request, strconv.FormatInt(match.ID, 10), true)
			if err != nil {
				return sohaapi.ExternalPipelineRun{Status: "duplicate_runs"}, err
			}
			run.StopConfirmed = run.StopConfirmed && inspection.Run.StopConfirmed
		}
		return run, nil
	}
}

func pipelineStopped(status string) bool {
	switch strings.ToLower(status) {
	case "success", "failed", "canceled", "skipped":
		return true
	default:
		return false
	}
}

func pipelineJobStopped(status string) bool {
	return status == "manual" || pipelineStopped(status)
}
