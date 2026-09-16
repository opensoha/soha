package gitlab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type pipelineJob struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Pipeline   pipelineResponse  `json:"pipeline"`
	Downstream *pipelineResponse `json:"downstream_pipeline"`
}

type pipelineInspection struct {
	pipeline pipelineResponse
	jobs     []pipelineJob
	bridges  []pipelineJob
}

func (c *Client) InspectPipeline(ctx context.Context, request domainbuild.PipelineRequest, id string, stop bool) (domainbuild.PipelineInspection, error) {
	result := domainbuild.PipelineInspection{Run: sohaapi.ExternalPipelineRun{RunID: id, Status: "unknown"}}
	root, err := c.pipeline(ctx, request.Spec.ProviderProjectID, id)
	if err != nil {
		return result, err
	}
	result.Run = root.run()
	match, err := c.pipelineVariablesMatch(ctx, request, id)
	if err != nil {
		return result, err
	}
	if !match {
		return result, fmt.Errorf("%w: CI run does not belong to this task", apperrors.ErrConflict)
	}
	identityErr := validatePipelineIdentity(root, request)
	if identityErr != nil {
		result.Run.Status, stop = "identity_mismatch", true
	}
	graph, err := c.inspectPipelineGraph(ctx, root)
	if err != nil {
		return result, err
	}
	if pipelineGraphStopped(graph) {
		confirmed, err := c.inspectPipelineGraph(ctx, graph[0].pipeline)
		if err != nil {
			return result, err
		}
		// A terminal parent is insufficient while its jobs/children are changing.
		result.Run.StopConfirmed = pipelineGraphStopped(confirmed) && reflect.DeepEqual(graph, confirmed)
		graph = confirmed
	}
	stopConfirmed := result.Run.StopConfirmed
	result.Run = graph[0].pipeline.run()
	if identityErr != nil {
		result.Run.Status = "identity_mismatch"
	}
	result.Run.StopConfirmed = stopConfirmed
	if stop && !result.Run.StopConfirmed {
		return result, c.stopPipelineGraph(ctx, graph)
	}
	if identityErr != nil {
		return result, identityErr
	}
	if stop || !result.Run.StopConfirmed || result.Run.Status != "success" {
		return result, nil
	}
	return c.pipelineArtifact(ctx, request, result, graph[0].jobs)
}

// Read every attempt: a retried job must not hide an earlier runner still stopping.
func (c *Client) pipelineJobs(ctx context.Context, pipeline pipelineResponse, kind string) ([]pipelineJob, error) {
	var jobs []pipelineJob
	params := url.Values{"per_page": {"100"}, "include_retried": {"true"}}
	path := pipelineProjectPath(strconv.FormatInt(pipeline.ProjectID, 10)) + "/pipelines/" + strconv.FormatInt(pipeline.ID, 10) + "/" + kind
	for page := 1; page <= 20; page++ {
		params.Set("page", strconv.Itoa(page))
		data, err := c.readMetadata(ctx, path, params, 1<<20)
		if err != nil {
			return nil, err
		}
		var batch []pipelineJob
		if json.Unmarshal(data, &batch) != nil {
			return nil, fmt.Errorf("%w: invalid CI job list", apperrors.ErrClusterUnready)
		}
		for _, job := range batch {
			if job.ID <= 0 || job.Pipeline.ID != pipeline.ID || job.Pipeline.ProjectID != pipeline.ProjectID || job.Pipeline.SHA != pipeline.SHA {
				return nil, fmt.Errorf("%w: CI job identity changed", apperrors.ErrConflict)
			}
		}
		jobs = append(jobs, batch...)
		if len(batch) < 100 {
			return jobs, nil
		}
	}
	return nil, fmt.Errorf("%w: CI job inspection exceeded its limit", apperrors.ErrConflict)
}

func (c *Client) inspectPipelineGraph(ctx context.Context, root pipelineResponse) ([]pipelineInspection, error) {
	queue := []pipelineResponse{root}
	seen := map[string]bool{}
	var graph []pipelineInspection
	totalJobs := 0
	for len(queue) > 0 {
		pipeline := queue[0]
		queue = queue[1:]
		project, id := strconv.FormatInt(pipeline.ProjectID, 10), strconv.FormatInt(pipeline.ID, 10)
		key := project + "/" + id
		if seen[key] {
			continue
		}
		if pipeline.ProjectID <= 0 || pipeline.ID <= 0 || len(seen) >= 32 {
			return nil, fmt.Errorf("%w: invalid or excessive CI child pipelines", apperrors.ErrConflict)
		}
		seen[key] = true
		current, err := c.pipeline(ctx, project, id)
		if err != nil {
			return nil, err
		}
		if current.SHA != pipeline.SHA {
			return nil, fmt.Errorf("%w: CI child identity changed", apperrors.ErrConflict)
		}
		jobs, err := c.pipelineJobs(ctx, current, "jobs")
		if err != nil {
			return nil, err
		}
		// bridges is supported by existing self-managed versions and GitLab 19.2+.
		bridges, err := c.pipelineJobs(ctx, current, "bridges")
		if err != nil {
			return nil, err
		}
		totalJobs += len(jobs) + len(bridges)
		if totalJobs > 2000 {
			return nil, fmt.Errorf("%w: CI graph exceeded its job limit", apperrors.ErrConflict)
		}
		graph = append(graph, pipelineInspection{pipeline: current, jobs: jobs, bridges: bridges})
		for _, bridge := range bridges {
			if bridge.Downstream != nil {
				queue = append(queue, *bridge.Downstream)
			}
		}
	}
	return graph, nil
}

func pipelineGraphStopped(graph []pipelineInspection) bool {
	for _, item := range graph {
		if !pipelineStopped(item.pipeline.Status) {
			return false
		}
		for _, jobs := range [][]pipelineJob{item.jobs, item.bridges} {
			for _, job := range jobs {
				if !pipelineJobStopped(job.Status) {
					return false
				}
			}
		}
	}
	return len(graph) > 0
}

func (c *Client) stopPipelineGraph(ctx context.Context, graph []pipelineInspection) error {
	for _, item := range graph {
		project := pipelineProjectPath(strconv.FormatInt(item.pipeline.ProjectID, 10))
		needsCancel := !pipelineStopped(item.pipeline.Status)
		for _, bridge := range item.bridges {
			needsCancel = needsCancel || !pipelineJobStopped(bridge.Status)
		}
		if needsCancel {
			if _, _, err := c.writePipeline(ctx, project+"/pipelines/"+strconv.FormatInt(item.pipeline.ID, 10)+"/cancel", []byte("{}")); err != nil {
				return err
			}
		}
		for _, job := range item.jobs {
			if !pipelineJobStopped(job.Status) {
				if _, _, err := c.writePipeline(ctx, project+"/jobs/"+strconv.FormatInt(job.ID, 10)+"/cancel", []byte("{}")); err != nil {
					return err
				}
			}
		}
	}
	// POST acknowledgment is not stop confirmation; the next GET must observe it.
	return nil
}

func (c *Client) pipelineArtifact(ctx context.Context, request domainbuild.PipelineRequest, result domainbuild.PipelineInspection, jobs []pipelineJob) (domainbuild.PipelineInspection, error) {
	var selected pipelineJob
	for _, job := range jobs {
		if job.Name == request.Spec.Configuration.ArtifactJob && job.ID > selected.ID {
			selected = job
		}
	}
	if selected.ID == 0 || selected.Status != "success" {
		return result, fmt.Errorf("%w: successful CI artifact job is missing", apperrors.ErrConflict)
	}
	jobID := strconv.FormatInt(selected.ID, 10)
	data, err := c.readMetadata(ctx, pipelineProjectPath(request.Spec.ProviderProjectID)+"/jobs/"+jobID+"/artifacts/soha-artifact.json", nil, 64<<10)
	if err != nil {
		return result, err
	}
	report, err := validatePipelineArtifact(data, request, result.Run.RunID, jobID)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(data)
	result.Run.ArtifactDigest, result.Run.ArtifactJobID = "sha256:"+hex.EncodeToString(sum[:]), jobID
	result.Artifact, result.ImageDigest = data, report.ImageDigest
	return result, nil
}

func validatePipelineArtifact(data []byte, request domainbuild.PipelineRequest, runID, jobID string) (sohaapi.ExternalPipelineArtifactReport, error) {
	var report sohaapi.ExternalPipelineArtifactReport
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil || decoder.Decode(new(any)) != io.EOF {
		return report, fmt.Errorf("%w: invalid CI artifact report", apperrors.ErrConflict)
	}
	if report.Version != 1 || report.TaskID != request.TaskID || report.PipelineID != runID || report.JobID != jobID || report.SourceCommit != request.Spec.SourceCommit || report.PipelineCommit != request.Spec.PipelineCommit || report.Image != request.Image {
		return report, fmt.Errorf("%w: CI artifact does not match the frozen task", apperrors.ErrConflict)
	}
	if len(report.ImageDigest) != 71 || report.ImageDigest[:7] != "sha256:" {
		return report, fmt.Errorf("%w: CI image requires a SHA-256 digest", apperrors.ErrConflict)
	}
	if _, err := hex.DecodeString(report.ImageDigest[7:]); err != nil {
		return report, fmt.Errorf("%w: invalid CI image digest", apperrors.ErrConflict)
	}
	return report, nil
}
