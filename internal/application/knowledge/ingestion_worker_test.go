package knowledge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type ingestionRepositoryStub struct {
	Repository
	documents []domainknowledge.Document
	base      domainknowledge.KnowledgeBase
	baseErr   error
	source    domainknowledge.Source
	sourceErr error
}

func (r *ingestionRepositoryStub) GetBase(context.Context, domainknowledge.PrincipalScope, string) (domainknowledge.KnowledgeBase, error) {
	return r.base, r.baseErr
}

func (r *ingestionRepositoryStub) GetSource(context.Context, domainknowledge.PrincipalScope, string, string) (domainknowledge.Source, error) {
	return r.source, r.sourceErr
}

type recordingVectorScorer struct {
	batches   []int
	failAt    int
	model     string
	afterCall func()
}

func (s *recordingVectorScorer) Embed(_ context.Context, _ domainidentity.Principal, inputs []string) (domainknowledge.EmbeddingBatch, error) {
	s.batches = append(s.batches, len(inputs))
	if s.afterCall != nil {
		s.afterCall()
	}
	if len(s.batches) == s.failAt {
		return domainknowledge.EmbeddingBatch{}, errors.New("model timeout")
	}
	vectors := make([][]float64, len(inputs))
	for i := range vectors {
		vectors[i] = []float64{1, 0}
	}
	return domainknowledge.EmbeddingBatch{Model: firstNonEmpty(s.model, "fixture"), Vectors: vectors}, nil
}

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
	staged    map[string][]domainknowledge.Chunk
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
	chunks []domainknowledge.Chunk,
) error {
	if r.staged == nil {
		r.staged = map[string][]domainknowledge.Chunk{}
	}
	r.staged[document.ID] = slices.Clone(chunks)
	for i := range r.documents {
		if r.documents[i].ID == document.ID {
			r.documents[i] = document
			return nil
		}
	}
	r.documents = append(r.documents, document)
	return nil
}

func (r *productionRepositoryStub) LoadIngestionDocument(_ context.Context, _, _ string, document domainknowledge.Document) ([]domainknowledge.Chunk, error) {
	for _, staged := range r.documents {
		if staged.ID == document.ID && staged.ContentHash == document.ContentHash {
			return slices.Clone(r.staged[document.ID]), nil
		}
	}
	return nil, nil
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

func TestPrepareIngestionRequiresExplicitTombstones(t *testing.T) {
	source := domainknowledge.Source{ID: "source-1", KnowledgeBaseID: "base-1"}
	empty, err := prepareIngestion(source, "owner", nil, "next", time.Now)
	if err != nil || len(empty.documents) != 0 {
		t.Fatalf("empty delta invented deletions: %+v %v", empty, err)
	}
	deleted, err := prepareIngestion(source, "owner", []domainknowledge.SourceDocument{{ExternalID: "runbook", Title: "Runbook", Deleted: true}}, "next", time.Now)
	if err != nil || len(deleted.documents) != 1 || deleted.documents[0].Status != domainknowledge.DocumentStatusDeleted || len(deleted.chunks[0]) != 0 {
		t.Fatalf("tombstone lost: %+v %v", deleted, err)
	}
	for _, documents := range [][]domainknowledge.SourceDocument{
		{{ExternalID: "runbook", Content: "must not delete", Deleted: true}},
		{{ExternalID: "runbook", Content: "one"}, {ExternalID: "runbook", Deleted: true}},
		{{ExternalID: "runbook"}},
	} {
		if _, err := prepareIngestion(source, "owner", documents, "", time.Now); !errors.Is(err, domainknowledge.ErrInvalidInput) {
			t.Fatalf("ambiguous batch accepted: %v", err)
		}
	}
}

func TestImportedACLDoesNotGrantBaseOwner(t *testing.T) {
	t.Parallel()
	for _, scope := range []domainknowledge.AccessScope{
		{Visibility: "private"},
		{Visibility: "private", Users: []string{" source-reader ", "source-reader"}},
		{Visibility: "private", Roles: []string{"source-role"}},
		{Visibility: "private", Teams: []string{"source-team"}},
		{Visibility: "private", Projects: []string{"source-project"}},
		{Visibility: "scoped", Users: []string{"source-reader"}},
		{Visibility: "public"},
	} {
		prepared, err := prepareIngestion(domainknowledge.Source{ID: "source", KnowledgeBaseID: "base"}, "base-owner", []domainknowledge.SourceDocument{{ExternalID: "private", Content: "source-private text", ACL: scope}}, "", time.Now)
		if err != nil {
			t.Fatal(err)
		}
		acl := prepared.documents[0].ACL
		if acl.Visibility != scope.Visibility || !slices.Equal(acl.Users, uniqueStrings(scope.Users)) || !slices.Equal(acl.Roles, scope.Roles) || !slices.Equal(acl.Teams, scope.Teams) || !slices.Equal(acl.Projects, scope.Projects) {
			t.Fatalf("import expanded explicit source ACL: %+v", acl)
		}
	}
}

func TestKnowledgeBaseKeepsContractScopedVisibility(t *testing.T) {
	t.Parallel()
	base, err := normalizeBaseInput(domainknowledge.BaseInput{Name: "Shared knowledge", Scope: domainknowledge.AccessScope{Visibility: "scoped", Users: []string{"reader"}}}, domainidentity.Principal{UserID: "base-owner"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if base.OwnerID != "base-owner" || base.Scope.Visibility != "scoped" || !slices.Equal(base.Scope.Users, []string{"reader"}) {
		t.Fatalf("scoped base contract changed: %+v", base)
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
	baseRepo := &ingestionRepositoryStub{base: domainknowledge.KnowledgeBase{ID: "base-1", OwnerID: "base-owner"}}
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
	baseRepo.source = productionRepo.source
	service := newIngestionTestService(t, baseRepo, productionRepo, &recordingVectorScorer{}, &job)
	service.loader = inlineLoaderStub{}
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
	if !slices.Equal(productionRepo.documents[0].ACL.Users, []string{"base-owner"}) {
		t.Fatalf("private document ownership changed to syncing operator: %+v", productionRepo.documents[0].ACL)
	}
}

type syncRepositoryStub struct {
	ingestionRepositoryStub
	runs []domainknowledge.SyncRun
}

func (r *syncRepositoryStub) CreateSyncRun(_ context.Context, run domainknowledge.SyncRun) error {
	r.runs = append(r.runs, run)
	return nil
}

func (r *syncRepositoryStub) UpdateSyncRun(_ context.Context, run domainknowledge.SyncRun) error {
	r.runs[len(r.runs)-1] = run
	return nil
}

func (r *syncRepositoryStub) UpdateSource(_ context.Context, _ domainknowledge.PrincipalScope, source domainknowledge.Source) error {
	r.source = source
	return nil
}

func (*syncRepositoryStub) CreateIndexRevision(context.Context, domainknowledge.IndexRevision) error {
	return nil
}

func TestSyncSourceKeepsPrivateOwnerAcrossOperators(t *testing.T) {
	t.Parallel()
	repo := &syncRepositoryStub{ingestionRepositoryStub: ingestionRepositoryStub{
		base:   domainknowledge.KnowledgeBase{ID: "base-1", OwnerID: "base-owner"},
		source: domainknowledge.Source{ID: "source-1", KnowledgeBaseID: "base-1", Kind: domainknowledge.SourceKindInline},
	}}
	permissions := appaccess.NewPermissionResolver(testRoleReader{permissions: map[string][]string{"operator": {PermKnowledgeIngestionOperate}}})
	service, err := New(repo, permissions, nil, nil, WithSourceLoader(inlineLoaderStub{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"sync-one", "sync-two"} {
		run, err := service.SyncSource(t.Context(), domainidentity.Principal{UserID: userID, Roles: []string{"operator"}}, repo.base.ID, repo.source.ID)
		if err != nil || run.Status != domainknowledge.RunStatusSucceeded {
			t.Fatalf("sync %s failed: %s %v", userID, run.Status, err)
		}
	}
	if len(repo.documents) != 2 || repo.documents[0].ID != repo.documents[1].ID {
		t.Fatalf("sync changed document identity: %+v", repo.documents)
	}
	for _, document := range repo.documents {
		if document.ACL.Visibility != "private" || !slices.Equal(document.ACL.Users, []string{"base-owner"}) {
			t.Fatalf("sync changed private ownership: %+v", document.ACL)
		}
	}
	repo.baseErr = domainknowledge.ErrBaseNotFound
	if _, err := service.SyncSource(t.Context(), domainidentity.Principal{UserID: "sync-two", Roles: []string{"operator"}}, repo.base.ID, repo.source.ID); !errors.Is(err, repo.baseErr) || len(repo.runs) != 2 || len(repo.documents) != 2 {
		t.Fatalf("unavailable base allowed sync side effects: err=%v runs=%d documents=%d", err, len(repo.runs), len(repo.documents))
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
	service := newIngestionTestService(t, &ingestionRepositoryStub{}, productionRepo, scorer, &job)
	service.now = func() time.Time { return now }
	chunks := make([]domainknowledge.Chunk, 600)
	execution := &ingestionExecution{
		job:        &job,
		prepared:   preparedIngestion{chunks: [][]domainknowledge.Chunk{chunks}},
		checkpoint: job.Checkpoint,
	}
	execution.prepared.documents = []domainknowledge.Document{{ID: "doc", KnowledgeBaseID: job.KnowledgeBaseID}}
	if err := service.embedIngestion(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	want := []int{1, 255, 255, 89}
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

type ingestionPrincipalStub struct {
	principal domainidentity.Principal
	err       error
	tokenID   string
}

func (s *ingestionPrincipalStub) CurrentExecutionPrincipal(_ context.Context, _, tokenID string) (domainidentity.Principal, error) {
	s.tokenID = tokenID
	return s.principal, s.err
}

func newIngestionTestService(t *testing.T, base *ingestionRepositoryStub, production *productionRepositoryStub, scorer *recordingVectorScorer, job *domainknowledge.IngestionJob) *Service {
	t.Helper()
	principal := domainidentity.Principal{UserID: "owner", Roles: []string{"operator"}}
	keys := []string{PermKnowledgeIngestionOperate}
	job.PrincipalSnapshot = domainknowledge.IngestionPrincipalSnapshot{UserID: principal.UserID, Roles: principal.Roles, PermissionKeys: keys, AccessTokenID: "frozen-token"}
	production.job = *job
	permissions := appaccess.NewPermissionResolver(testRoleReader{permissions: map[string][]string{"operator": keys}})
	service, err := New(base, permissions, nil, scorer, WithProductionRepository(production), WithExecutionPrincipalReader(&ingestionPrincipalStub{principal: principal}))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestIngestionResumesPersistedEmbeddingBatches(t *testing.T) {
	for _, changedModel := range []bool{false, true} {
		t.Run(fmt.Sprintf("model_changed_%t", changedModel), func(t *testing.T) {
			job := domainknowledge.IngestionJob{ID: "resumable", Stage: domainknowledge.IngestionStageChunking, Status: domainknowledge.IngestionJobRunning, LeaseToken: "lease-1", Attempt: 1, MaxAttempts: 3}
			repo := &productionRepositoryStub{job: job}
			scorer := &recordingVectorScorer{failAt: 3}
			service := newIngestionTestService(t, &ingestionRepositoryStub{}, repo, scorer, &job)
			document := domainknowledge.Document{ID: "doc", ContentHash: "unchanged"}
			fresh := func() preparedIngestion {
				chunks := make([]domainknowledge.Chunk, 600)
				for i := range chunks {
					chunks[i] = domainknowledge.Chunk{DocumentID: document.ID, Ordinal: i, Content: fmt.Sprint(i), ContentHash: fmt.Sprint(i)}
				}
				return preparedIngestion{documents: []domainknowledge.Document{document}, chunks: [][]domainknowledge.Chunk{chunks}}
			}
			execution := &ingestionExecution{job: &job, prepared: fresh()}
			if err := service.embedIngestion(t.Context(), execution); err == nil || job.Status != domainknowledge.IngestionJobRetryWait {
				t.Fatalf("failure/retry state = %v %s", err, job.Status)
			}
			persisted := 0
			for _, chunk := range repo.staged[document.ID] {
				if len(chunk.Embedding) > 0 {
					persisted++
				}
			}
			if persisted != 256 {
				t.Fatalf("persisted vectors = %d", persisted)
			}
			job.Status, job.Stage, job.LeaseToken, job.Attempt = domainknowledge.IngestionJobRunning, domainknowledge.IngestionStageChunking, "lease-2", 2
			repo.job = job
			scorer.batches, scorer.failAt = nil, 0
			if changedModel {
				scorer.model = "fixture-v2"
			}
			execution = &ingestionExecution{job: &job, prepared: fresh()}
			if err := service.embedIngestion(t.Context(), execution); err != nil {
				t.Fatal(err)
			}
			want := []int{1, 255, 89}
			if changedModel {
				want = []int{1, 255, 255, 89}
			}
			if !slices.Equal(scorer.batches, want) {
				t.Fatalf("resume batches = %v, want %v", scorer.batches, want)
			}
			for _, chunk := range execution.prepared.chunks[0] {
				if !validIngestionVector(chunk.Embedding, 2) || chunk.EmbeddingModel != firstNonEmpty(scorer.model, "fixture") {
					t.Fatalf("invalid restored vector: %+v", chunk)
				}
			}
		})
	}
}

func TestIngestionRevocationStopsBeforeAnotherModelCall(t *testing.T) {
	job := domainknowledge.IngestionJob{ID: "revoked", Stage: domainknowledge.IngestionStageChunking, Status: domainknowledge.IngestionJobRunning, LeaseToken: "lease", Attempt: 1, MaxAttempts: 3}
	repo := &productionRepositoryStub{job: job}
	scorer := &recordingVectorScorer{}
	service := newIngestionTestService(t, &ingestionRepositoryStub{}, repo, scorer, &job)
	reader, ok := service.principals.(*ingestionPrincipalStub)
	if !ok {
		t.Fatal("expected ingestion principal fixture")
	}
	scorer.afterCall = func() { reader.err = errors.New("token revoked") }
	execution := &ingestionExecution{job: &job, prepared: preparedIngestion{documents: []domainknowledge.Document{{ID: "doc"}}, chunks: [][]domainknowledge.Chunk{{{Content: "one"}, {Content: "two"}}}}}
	if err := service.embedIngestion(t.Context(), execution); err == nil || job.Status != domainknowledge.IngestionJobFailed || job.ErrorCode != "authorization_revoked" {
		t.Fatalf("revocation state = %v %s %s", err, job.Status, job.ErrorCode)
	}
	if len(scorer.batches) != 1 || repo.published || reader.tokenID != "frozen-token" {
		t.Fatalf("model calls/publication/token = %v %t %q", scorer.batches, repo.published, reader.tokenID)
	}
}

type inlineLoaderStub struct{}

func (inlineLoaderStub) Load(context.Context, domainidentity.Principal, domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error) {
	return []domainknowledge.SourceDocument{{ExternalID: "runbook", Title: "Runbook", Content: "rollback the deployment"}}, "v1:cursor", nil
}
