package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type ingestionRepositoryStub struct {
	Repository
	documents []domainknowledge.Document
}

type recordingVectorScorer struct{ batches []int }

func (s *recordingVectorScorer) Score(_ context.Context, _ domainidentity.Principal, _ string, chunks []domainknowledge.Chunk) ([]float64, error) {
	s.batches = append(s.batches, len(chunks))
	return make([]float64, len(chunks)), nil
}

func (r *ingestionRepositoryStub) UpsertDocument(_ context.Context, document domainknowledge.Document, _ []domainknowledge.Chunk) error {
	r.documents = append(r.documents, document)
	return nil
}

type productionRepositoryStub struct {
	ProductionRepository
	job       domainknowledge.IngestionJob
	source    domainknowledge.Source
	documents []domainknowledge.Document
	published bool
}

func (r *productionRepositoryStub) GetSourceInternal(context.Context, string, string) (domainknowledge.Source, error) {
	return r.source, nil
}

func (r *productionRepositoryStub) GetIngestionJobInternal(context.Context, string) (domainknowledge.IngestionJob, error) {
	return r.job, nil
}

func (r *productionRepositoryStub) AdvanceIngestionJob(
	_ context.Context,
	job domainknowledge.IngestionJob,
	expectedStatus domainknowledge.IngestionJobStatus,
	expectedStage domainknowledge.IngestionStage,
) error {
	if r.job.Status != expectedStatus || r.job.Stage != expectedStage || r.job.LeaseToken != job.LeaseToken {
		return domainknowledge.ErrIngestionConflict
	}
	r.job = job
	return nil
}

func (r *productionRepositoryStub) StageIngestionDocument(
	_ context.Context,
	_ string,
	_ string,
	document domainknowledge.Document,
	_ []domainknowledge.Chunk,
) error {
	r.documents = append(r.documents, document)
	return nil
}

func (r *productionRepositoryStub) PublishIngestionJob(
	_ context.Context,
	job domainknowledge.IngestionJob,
	_ domainknowledge.Source,
	_ domainknowledge.IndexRevision,
) error {
	if r.job.Status != domainknowledge.IngestionJobRunning || r.job.Stage != domainknowledge.IngestionStagePublishing {
		return domainknowledge.ErrIngestionConflict
	}
	r.job = job
	r.published = true
	return nil
}

func TestValidateIngestionTransition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		fromStatus domainknowledge.IngestionJobStatus
		fromStage  domainknowledge.IngestionStage
		toStatus   domainknowledge.IngestionJobStatus
		toStage    domainknowledge.IngestionStage
		valid      bool
	}{
		{
			name:       "next stage",
			fromStatus: domainknowledge.IngestionJobRunning,
			fromStage:  domainknowledge.IngestionStageFetching,
			toStatus:   domainknowledge.IngestionJobRunning,
			toStage:    domainknowledge.IngestionStageParsing,
			valid:      true,
		},
		{
			name:       "retry from current stage",
			fromStatus: domainknowledge.IngestionJobRunning,
			fromStage:  domainknowledge.IngestionStageEmbedding,
			toStatus:   domainknowledge.IngestionJobRetryWait,
			toStage:    domainknowledge.IngestionStageDiscovering,
			valid:      true,
		},
		{
			name:       "cancel acknowledgement",
			fromStatus: domainknowledge.IngestionJobCancelling,
			fromStage:  domainknowledge.IngestionStageIndexing,
			toStatus:   domainknowledge.IngestionJobCancelled,
			toStage:    domainknowledge.IngestionStageIndexing,
			valid:      true,
		},
		{
			name:       "skip stage rejected",
			fromStatus: domainknowledge.IngestionJobRunning,
			fromStage:  domainknowledge.IngestionStageFetching,
			toStatus:   domainknowledge.IngestionJobRunning,
			toStage:    domainknowledge.IngestionStageEmbedding,
		},
		{
			name:       "terminal callback rejected",
			fromStatus: domainknowledge.IngestionJobSucceeded,
			fromStage:  domainknowledge.IngestionStagePublishing,
			toStatus:   domainknowledge.IngestionJobRunning,
			toStage:    domainknowledge.IngestionStagePublishing,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateIngestionTransition(test.fromStatus, test.fromStage, test.toStatus, test.toStage)
			if test.valid && err != nil {
				t.Fatalf("transition error = %v", err)
			}
			if !test.valid && !errors.Is(err, domainknowledge.ErrIngestionConflict) {
				t.Fatalf("transition error = %v, want conflict", err)
			}
		})
	}
}

func TestPrepareIngestionKeepsDocumentsPendingUntilPublish(t *testing.T) {
	t.Parallel()
	prepared, err := prepareIngestion(
		domainknowledge.Source{ID: "source-1", KnowledgeBaseID: "base-1"},
		"owner-1",
		[]domainknowledge.SourceDocument{{ExternalID: "runbook", Title: "Runbook", Content: "rollback the deployment"}},
		"v1:cursor",
		func() time.Time { return time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.documents) != 1 || prepared.documents[0].Status != domainknowledge.DocumentStatusPending {
		t.Fatalf("prepared documents = %#v", prepared.documents)
	}
	if len(prepared.chunks) != 1 || len(prepared.chunks[0]) == 0 || prepared.hash == "" {
		t.Fatalf("prepared chunks/hash = %#v %q", prepared.chunks, prepared.hash)
	}
}

func TestExecuteIngestionPublishesOnlyAfterAllStages(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC)
	job := domainknowledge.IngestionJob{
		ID:              "job-1",
		KnowledgeBaseID: "base-1",
		SourceID:        "source-1",
		TargetRevision:  1,
		Stage:           domainknowledge.IngestionStageDiscovering,
		Status:          domainknowledge.IngestionJobRunning,
		Attempt:         1,
		MaxAttempts:     3,
		LeaseToken:      "lease-1",
		Checkpoint: domainknowledge.IngestionCheckpoint{
			Stage:      domainknowledge.IngestionStageDiscovering,
			RecordedAt: now,
		},
	}
	baseRepo := &ingestionRepositoryStub{}
	productionRepo := &productionRepositoryStub{
		job: job,
		source: domainknowledge.Source{
			ID:              "source-1",
			KnowledgeBaseID: "base-1",
			Kind:            domainknowledge.SourceKindInline,
			Config: map[string]any{"documents": []map[string]any{{
				"externalId": "runbook",
				"title":      "Runbook",
				"content":    "rollback the deployment",
			}}},
		},
	}
	service, err := New(
		baseRepo,
		nil,
		nil,
		nil,
		WithProductionRepository(productionRepo),
		WithSourceLoader(inlineLoaderStub{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	if err := service.executeIngestion(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
	if !productionRepo.published || productionRepo.job.Status != domainknowledge.IngestionJobSucceeded {
		t.Fatalf("published=%t job=%#v", productionRepo.published, productionRepo.job)
	}
	if len(productionRepo.documents) != 1 || productionRepo.documents[0].Status != domainknowledge.DocumentStatusPending {
		t.Fatalf("staged documents = %#v", productionRepo.documents)
	}
}

func TestEmbedIngestionUsesBoundedBatchesAndRenewsLease(t *testing.T) {
	now := time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC)
	job := domainknowledge.IngestionJob{
		ID: "job-batched", KnowledgeBaseID: "base-1", SourceID: "source-1",
		Stage: domainknowledge.IngestionStageChunking, Status: domainknowledge.IngestionJobRunning,
		Attempt: 1, MaxAttempts: 3, LeaseToken: "lease-1",
		Checkpoint: domainknowledge.IngestionCheckpoint{Stage: domainknowledge.IngestionStageChunking, RecordedAt: now},
	}
	productionRepo := &productionRepositoryStub{job: job}
	scorer := &recordingVectorScorer{}
	service, err := New(&ingestionRepositoryStub{}, nil, nil, scorer, WithProductionRepository(productionRepo))
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	chunks := make([]domainknowledge.Chunk, 600)
	execution := &ingestionExecution{
		job:        &job,
		prepared:   preparedIngestion{chunks: [][]domainknowledge.Chunk{chunks}},
		checkpoint: job.Checkpoint,
	}
	if err := service.embedIngestion(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	want := []int{255, 255, 90}
	if len(scorer.batches) != len(want) {
		t.Fatalf("embedding batches = %#v", scorer.batches)
	}
	for index := range want {
		if scorer.batches[index] != want[index] {
			t.Fatalf("embedding batches = %#v", scorer.batches)
		}
	}
	if job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		t.Fatalf("lease expiry = %v", job.LeaseExpiresAt)
	}
}

type inlineLoaderStub struct{}

func (inlineLoaderStub) Load(context.Context, domainidentity.Principal, domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error) {
	return []domainknowledge.SourceDocument{{ExternalID: "runbook", Title: "Runbook", Content: "rollback the deployment"}}, "v1:cursor", nil
}
