package knowledge

import (
	"context"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
)

type Repository interface {
	ListBases(context.Context, domainknowledge.PrincipalScope) ([]domainknowledge.KnowledgeBase, error)
	GetBase(context.Context, domainknowledge.PrincipalScope, string) (domainknowledge.KnowledgeBase, error)
	CreateBase(context.Context, domainknowledge.KnowledgeBase) (domainknowledge.KnowledgeBase, error)
	UpdateBase(context.Context, domainknowledge.PrincipalScope, domainknowledge.KnowledgeBase) (domainknowledge.KnowledgeBase, error)
	DeleteBase(context.Context, domainknowledge.PrincipalScope, string) error
	ListSources(context.Context, domainknowledge.PrincipalScope, string) ([]domainknowledge.Source, error)
	GetSource(context.Context, domainknowledge.PrincipalScope, string, string) (domainknowledge.Source, error)
	CreateSource(context.Context, domainknowledge.PrincipalScope, domainknowledge.Source) (domainknowledge.Source, error)
	UpdateSource(context.Context, domainknowledge.PrincipalScope, domainknowledge.Source) error
	ListDocuments(context.Context, domainknowledge.PrincipalScope, string, int) ([]domainknowledge.Document, error)
	UpsertDocument(context.Context, domainknowledge.Document, []domainknowledge.Chunk) error
	ListAuthorizedChunks(context.Context, domainknowledge.PrincipalScope, domainknowledge.SearchRequest, int) ([]domainknowledge.Chunk, error)
	CreateSyncRun(context.Context, domainknowledge.SyncRun) error
	UpdateSyncRun(context.Context, domainknowledge.SyncRun) error
	ListSyncRuns(context.Context, domainknowledge.PrincipalScope, string, int) ([]domainknowledge.SyncRun, error)
	CreateIndexRevision(context.Context, domainknowledge.IndexRevision) error
	ListIndexRevisions(context.Context, domainknowledge.PrincipalScope, string, int) ([]domainknowledge.IndexRevision, error)
}

type ProductionRepository interface {
	ListConnectors(context.Context, domainknowledge.PrincipalScope, int) ([]domainknowledge.ConnectorDefinition, error)
	GetConnector(context.Context, domainknowledge.PrincipalScope, string) (domainknowledge.ConnectorDefinition, error)
	CreateIngestionJob(context.Context, domainknowledge.IngestionJob) error
	GetIngestionJob(context.Context, domainknowledge.PrincipalScope, string) (domainknowledge.IngestionJob, error)
	GetIngestionJobInternal(context.Context, string) (domainknowledge.IngestionJob, error)
	ClaimIngestionJob(context.Context, time.Time, string, time.Time) (*domainknowledge.IngestionJob, error)
	AdvanceIngestionJob(context.Context, domainknowledge.IngestionJob, domainknowledge.IngestionJobStatus, domainknowledge.IngestionStage) error
	RequestIngestionCancel(context.Context, domainknowledge.PrincipalScope, string, time.Time) (domainknowledge.IngestionJob, error)
	RetryIngestionJob(context.Context, domainknowledge.PrincipalScope, string, time.Time) (domainknowledge.IngestionJob, error)
	GetSourceInternal(context.Context, string, string) (domainknowledge.Source, error)
	StageIngestionDocument(context.Context, string, string, domainknowledge.Document, []domainknowledge.Chunk) error
	PublishIngestionJob(context.Context, domainknowledge.IngestionJob, domainknowledge.Source, domainknowledge.IndexRevision) error
}

type SourceLoader interface {
	Load(context.Context, domainidentity.Principal, domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error)
}

type ConnectorValidator interface {
	Validate(context.Context, domainknowledge.ConnectorInput) (domainknowledge.ConnectorValidationResult, error)
}

type LexicalScorer interface {
	Score(query string, chunks []domainknowledge.Chunk) []float64
}

type VectorScorer interface {
	Score(context.Context, domainidentity.Principal, string, []domainknowledge.Chunk) ([]float64, error)
}

type Reranker interface {
	Rerank(context.Context, domainidentity.Principal, string, []domainknowledge.SearchHit) ([]domainknowledge.SearchHit, error)
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}
