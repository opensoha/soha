package systemintegration

import (
	"context"
	"errors"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type analysisReader struct {
	domainapp.SourceMetadataReader
	files   map[string]string
	err     error
	refErr  error
	reads   int
	commits []string
}

func (r *analysisReader) ResolveRepositoryRef(context.Context, string, string, string) (string, error) {
	return strings.Repeat("a", 40), r.refErr
}
func (r *analysisReader) ReadRepositoryFile(_ context.Context, _, commit, path string, limit int64) ([]byte, error) {
	r.reads++
	r.commits = append(r.commits, commit)
	if r.err != nil {
		return nil, r.err
	}
	content, ok := r.files[path]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	if int64(len(content)) > limit {
		return nil, domainapp.ErrSourceFileTooLarge
	}
	return []byte(content), nil
}

func TestRepositoryAnalysisFreezesCommitAndDistinguishesReadFailures(t *testing.T) {
	input := sohaapi.RepositoryAnalysisInput{RepositoryID: "repo", RefType: sohaapi.AnalysisRefBranch, RefName: "main", ProjectPath: "services/api"}
	reader := &analysisReader{files: map[string]string{"services/api/go.mod": "module example.test/api\ngo 1.26.0\n"}}
	result := analyzeRepositoryMetadata(context.Background(), reader, "42", input)
	if result.Status != sohaapi.AnalysisIdentified || result.Candidates[0].ProjectPath != "services/api" || reader.reads != 16 {
		t.Fatalf("result: %+v; reads=%d", result, reader.reads)
	}
	for _, commit := range reader.commits {
		if commit != strings.Repeat("a", 40) {
			t.Fatal("moving reference used")
		}
	}
	for _, tc := range []struct {
		err    error
		status sohaapi.RepositoryAnalysisStatus
		code   string
	}{
		{apperrors.ErrAccessDenied, sohaapi.AnalysisForbidden, "source_access_denied"},
		{context.DeadlineExceeded, sohaapi.AnalysisReadFailed, "analysis_timeout"},
		{domainapp.ErrSourceFileTooLarge, sohaapi.AnalysisReadFailed, "file_size_limit"},
		{errors.New("secret source content"), sohaapi.AnalysisReadFailed, "provider_read_failed"},
	} {
		result = analyzeRepositoryMetadata(context.Background(), &analysisReader{err: tc.err}, "42", input)
		if result.Status != tc.status || result.Warnings[0].Code != tc.code || strings.Contains(result.Warnings[0].Message, "secret") {
			t.Errorf("failure classification: %+v", result)
		}
	}
	result = analyzeRepositoryMetadata(context.Background(), &analysisReader{refErr: apperrors.ErrNotFound}, "42", input)
	if result.Status != sohaapi.AnalysisReadFailed || result.Warnings[0].Code != "source_not_found" {
		t.Fatalf("missing ref: %+v", result)
	}
	result = analyzeRepositoryMetadata(context.Background(), &analysisReader{}, "42", input)
	if result.Status != sohaapi.AnalysisUnrecognized || result.Warnings[0].Code != "no_candidate_files" {
		t.Fatalf("empty repo: %+v", result)
	}
	input.RefType, input.RefName = sohaapi.AnalysisRefCommit, strings.Repeat("b", 40)
	reader = &analysisReader{}
	result = analyzeRepositoryMetadata(context.Background(), reader, "42", input)
	if result.Status != sohaapi.AnalysisReadFailed || result.Warnings[0].Code != "commit_mismatch" || reader.reads != 0 {
		t.Fatalf("requested commit was replaced: %+v reads=%d", result, reader.reads)
	}
}

func TestRepositoryAnalysisRejectsTraversalAndCapsTotalBytes(t *testing.T) {
	input := sohaapi.RepositoryAnalysisInput{RepositoryID: "repo", RefType: sohaapi.AnalysisRefBranch, RefName: "main"}
	for _, path := range []string{"../app", "app/../other", "/tmp", "app\\other", "C:/app", "app\nname"} {
		input.ProjectPath = path
		if err := validateAnalysisInput(&input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Errorf("accepted %q", path)
		}
	}
	input.ProjectPath = "."
	reader := &analysisReader{files: map[string]string{}}
	for _, name := range []string{"Dockerfile", "go.mod", "go.work", "package.json", "package-lock.json"} {
		reader.files[name] = strings.Repeat("x", 256<<10)
	}
	result := analyzeRepositoryMetadata(context.Background(), reader, "42", input)
	if result.Status != sohaapi.AnalysisReadFailed || result.Warnings[0].Code != "total_size_limit" || reader.reads != 4 {
		t.Fatalf("budget: %+v reads=%d", result, reader.reads)
	}
}
