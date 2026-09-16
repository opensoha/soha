package gitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
)

func pipelineTestRequest() domainbuild.PipelineRequest {
	return domainbuild.PipelineRequest{TaskID: "task:test", Image: "registry.example/team/app:v1", CreatedAt: time.Now(), Spec: sohaapi.ExternalPipelineExecutionSpec{ProviderProjectID: "42", SourceCommit: strings.Repeat("a", 40), PipelineCommit: strings.Repeat("b", 40), Configuration: sohaapi.ExternalPipelineConfiguration{Provider: sohaapi.ExternalPipelineGitLab, PipelineTag: "soha-v1", ArtifactJob: "publish", RegistryID: "registry"}}}
}

func TestPipelineReconcilesLostResponseAndStopsChildren(t *testing.T) {
	request := pipelineTestRequest()
	root := pipelineResponse{ID: 101, ProjectID: 42, Ref: "soha-v1", SHA: request.Spec.PipelineCommit, Source: "api", Status: "canceled"}
	child := pipelineResponse{ID: 202, ProjectID: 43, Ref: "main", SHA: strings.Repeat("c", 40), Source: "parent_pipeline", Status: "running"}
	posts, rootPosts, childPosts, jobPosts := 0, 0, 0, 0
	stopped, foreign := false, false
	hideChildOnce := false
	var mu sync.Mutex
	server := newPipelineReconcileServer(t, request, &root, &child, &posts, &rootPosts, &childPosts, &jobPosts, &stopped, &foreign, &hideChildOnce, &mu)
	defer server.Close()
	client := NewWithOptions(Options{Enabled: true, BaseURL: server.URL, Token: "test-token"})
	run, err := client.StartPipeline(t.Context(), request)
	mu.Lock()
	if err == nil || run.StopConfirmed || posts != 1 {
		t.Fatalf("lost response was retried or considered stopped: %+v %v posts=%d", run, err, posts)
	}
	// Simulate the protected tag moving between the preflight GET and POST.
	root.SHA = strings.Repeat("d", 40)
	mu.Unlock()
	run, err = client.FindPipeline(t.Context(), request)
	if err == nil || run.RunID != "101" || run.Status != "identity_mismatch" {
		t.Fatalf("mismatched dispatch identity was lost: %+v %v", run, err)
	}
	inspection, _ := client.InspectPipeline(t.Context(), request, "101", true)
	mu.Lock()
	if inspection.Run.StopConfirmed || childPosts != 1 || jobPosts != 1 || rootPosts != 0 {
		t.Fatalf("child stop was not observed: %+v counts=%d/%d/%d", inspection, rootPosts, childPosts, jobPosts)
	}
	stopped, hideChildOnce = true, true
	mu.Unlock()
	inspection, _ = client.InspectPipeline(t.Context(), request, "101", true)
	if inspection.Run.StopConfirmed {
		t.Fatal("late child was hidden by the first terminal graph")
	}
	inspection, _ = client.InspectPipeline(t.Context(), request, "101", true)
	if !inspection.Run.StopConfirmed {
		t.Fatal("fully stopped graph was not confirmed")
	}
	mu.Lock()
	foreign = true
	mu.Unlock()
	inspection, err = client.InspectPipeline(t.Context(), request, "101", true)
	mu.Lock()
	defer mu.Unlock()
	if err == nil || inspection.Run.StopConfirmed || childPosts != 1 || jobPosts != 1 {
		t.Fatal("foreign task was accepted or canceled")
	}
}

func newPipelineReconcileServer(t *testing.T, request domainbuild.PipelineRequest, root, child *pipelineResponse, posts, rootPosts, childPosts, jobPosts *int, stopped, foreign, hideChildOnce *bool, mu *sync.Mutex) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Error("missing source authentication")
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v4")
		write := func(value any) {
			if err := json.NewEncoder(w).Encode(value); err != nil {
				t.Error(err)
			}
		}
		childState, childJobs := pipelineReconcileChild(*child, *stopped)
		switch path {
		case "/projects/42/repository/tags/soha-v1":
			write(map[string]any{"name": "soha-v1", "protected": true, "commit": map[string]string{"id": request.Spec.PipelineCommit}})
		case "/projects/42/pipeline":
			*posts++
			losePipelineDispatchResponse(t, w)
		case "/projects/42/pipelines":
			if r.URL.Query().Get("sha") != "" {
				t.Error("SHA filter can hide a definition-race dispatch")
			}
			write([]pipelineResponse{*root})
		case "/projects/42/pipelines/101/variables":
			write(pipelineReconcileVariables(request, *foreign))
		case "/projects/42/pipelines/101":
			write(root)
		case "/projects/43/pipelines/202":
			write(childState)
		case "/projects/42/pipelines/101/jobs":
			write([]pipelineJob{})
		case "/projects/42/pipelines/101/bridges":
			if *hideChildOnce {
				*hideChildOnce = false
				write([]pipelineJob{})
				return
			}
			write([]pipelineJob{{ID: 5, Status: "success", Pipeline: *root, Downstream: child}})
		case "/projects/43/pipelines/202/bridges":
			write([]pipelineJob{})
		case "/projects/43/pipelines/202/jobs":
			if r.URL.Query().Get("include_retried") != "true" {
				t.Error("retried jobs omitted")
			}
			write(childJobs)
		case "/projects/42/pipelines/101/cancel":
			*rootPosts++
			write(root)
		case "/projects/43/pipelines/202/cancel":
			*childPosts++
			write(child)
		case "/projects/43/jobs/303/cancel":
			(*jobPosts)++
			write(map[string]string{"status": "canceled"})
		default:
			t.Errorf("unexpected CI path %s", path)
			http.NotFound(w, r)
		}
	}))
}

func losePipelineDispatchResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Error("test server does not support hijacking")
		return
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	_ = connection.Close()
}

func pipelineReconcileVariables(request domainbuild.PipelineRequest, foreign bool) []map[string]string {
	values := []map[string]string{}
	for key, value := range pipelineVariables(request) {
		if foreign && key == "SOHA_TASK_ID" {
			value = "another-task"
		}
		values = append(values, map[string]string{"key": key, "value": value})
	}
	return values
}

func pipelineReconcileChild(child pipelineResponse, stopped bool) (pipelineResponse, []pipelineJob) {
	copy := child
	status := "canceling"
	if stopped {
		copy.Status, status = "canceled", "canceled"
	}
	return copy, []pipelineJob{{ID: 303, Status: status, Pipeline: child}}
}

func TestPipelineAcceptsOnlyActualJobArtifact(t *testing.T) {
	request := pipelineTestRequest()
	root := pipelineResponse{ID: 101, ProjectID: 42, Ref: "soha-v1", SHA: request.Spec.PipelineCommit, Source: "api", Status: "success"}
	report := sohaapi.ExternalPipelineArtifactReport{Version: 1, TaskID: request.TaskID, PipelineID: "101", JobID: "9", PipelineCommit: request.Spec.PipelineCommit, SourceCommit: request.Spec.SourceCommit, Image: request.Image, ImageDigest: "sha256:" + strings.Repeat("d", 64)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v4")
		var response any
		switch path {
		case "/projects/42/pipelines/101":
			response = root
		case "/projects/42/pipelines/101/variables":
			values := []map[string]string{}
			for key, value := range pipelineVariables(request) {
				values = append(values, map[string]string{"key": key, "value": value})
			}
			response = values
		case "/projects/42/pipelines/101/jobs":
			response = []pipelineJob{{ID: 8, Name: "publish", Status: "failed", Pipeline: root}, {ID: 9, Name: "publish", Status: "success", Pipeline: root}}
		case "/projects/42/pipelines/101/bridges":
			response = []pipelineJob{}
		case "/projects/42/jobs/9/artifacts/soha-artifact.json":
			response = report
		default:
			t.Error("unexpected artifact request", path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := NewWithOptions(Options{Enabled: true, BaseURL: server.URL, Token: "test-token"})
	inspection, err := client.InspectPipeline(t.Context(), request, "101", false)
	if err != nil || !inspection.Run.StopConfirmed || inspection.Run.ArtifactJobID != "9" || inspection.ImageDigest != report.ImageDigest {
		t.Fatalf("artifact rejected: %+v %v", inspection, err)
	}
	for _, field := range []string{"taskId", "pipelineId", "jobId", "sourceCommit", "pipelineCommit", "image", "imageDigest", "version"} {
		t.Run(field, func(t *testing.T) {
			data, _ := json.Marshal(report)
			var tampered map[string]any
			_ = json.Unmarshal(data, &tampered)
			tampered[field] = "different"
			data, _ = json.Marshal(tampered)
			if _, err := validatePipelineArtifact(data, request, "101", "9"); err == nil {
				t.Fatal("accepted wrong artifact", field)
			}
		})
	}
	data, _ := json.Marshal(report)
	if _, err := validatePipelineArtifact(append(data, []byte(`{}`)...), request, "101", "9"); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	if fmt.Sprint(pipelineVariables(domainbuild.PipelineRequest{Variables: map[string]string{"CI_CONFIG_PATH": "evil"}})["CI_CONFIG_PATH"]) != "" {
		t.Fatal("arbitrary CI variable forwarded")
	}
}
