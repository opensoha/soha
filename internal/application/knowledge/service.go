package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	PermKnowledgeView   = appaccess.PermAIKnowledgeView
	PermKnowledgeManage = appaccess.PermAIKnowledgeManage
	PermContextInspect  = appaccess.PermAIContextInspect
	maxCandidates       = 500
	maxListItems        = 200
)

type Service struct {
	repo               Repository
	productionRepo     ProductionRepository
	permissions        *appaccess.PermissionResolver
	loader             SourceLoader
	connectorValidator ConnectorValidator
	lexical            LexicalScorer
	vector             VectorScorer
	reranker           Reranker
	operations         OperationRecorder
	now                func() time.Time
}

type Option func(*Service)

func WithSourceLoader(loader SourceLoader) Option { return func(s *Service) { s.loader = loader } }
func WithReranker(reranker Reranker) Option       { return func(s *Service) { s.reranker = reranker } }
func WithProductionRepository(repo ProductionRepository) Option {
	return func(s *Service) { s.productionRepo = repo }
}
func WithConnectorValidator(validator ConnectorValidator) Option {
	return func(s *Service) { s.connectorValidator = validator }
}
func WithOperationRecorder(recorder OperationRecorder) Option {
	return func(s *Service) { s.operations = recorder }
}

func New(repo Repository, permissions *appaccess.PermissionResolver, lexical LexicalScorer, vector VectorScorer, options ...Option) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("knowledge repository is required")
	}
	if lexical == nil {
		lexical = LocalHybridScorer{}
	}
	if vector == nil {
		vector = LocalVectorScorer{}
	}
	service := &Service{repo: repo, permissions: permissions, lexical: lexical, vector: vector, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		option(service)
	}
	return service, nil
}

func (s *Service) SetRetrievalAdapters(vector VectorScorer, reranker Reranker) {
	if vector != nil {
		s.vector = vector
	}
	if reranker != nil {
		s.reranker = reranker
	}
}

func (s *Service) SetSourceLoader(loader SourceLoader) {
	if loader != nil {
		s.loader = loader
	}
}

func (s *Service) ListBases(ctx context.Context, principal domainidentity.Principal) ([]domainknowledge.KnowledgeBase, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return nil, err
	}
	return s.repo.ListBases(ctx, principalScope(principal))
}

func (s *Service) GetBase(ctx context.Context, principal domainidentity.Principal, baseID string) (domainknowledge.KnowledgeBase, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return domainknowledge.KnowledgeBase{}, err
	}
	return s.repo.GetBase(ctx, principalScope(principal), strings.TrimSpace(baseID))
}

func (s *Service) CreateBase(ctx context.Context, principal domainidentity.Principal, input domainknowledge.BaseInput) (domainknowledge.KnowledgeBase, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeManage); err != nil {
		return domainknowledge.KnowledgeBase{}, err
	}
	item, err := normalizeBaseInput(input, principal, s.now())
	if err != nil {
		return domainknowledge.KnowledgeBase{}, err
	}
	created, err := s.repo.CreateBase(ctx, item)
	s.record(ctx, principal, "knowledge.base.create", item.ID, item.Name, err)
	return created, err
}

func (s *Service) UpdateBase(ctx context.Context, principal domainidentity.Principal, baseID string, input domainknowledge.BaseInput) (domainknowledge.KnowledgeBase, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeManage); err != nil {
		return domainknowledge.KnowledgeBase{}, err
	}
	current, err := s.repo.GetBase(ctx, principalScope(principal), strings.TrimSpace(baseID))
	if err != nil {
		return domainknowledge.KnowledgeBase{}, err
	}
	updated, err := normalizeBaseInput(input, principal, s.now())
	if err != nil {
		return domainknowledge.KnowledgeBase{}, err
	}
	updated.ID, updated.OwnerID, updated.CreatedAt = current.ID, current.OwnerID, current.CreatedAt
	updated.Status = current.Status
	result, err := s.repo.UpdateBase(ctx, principalScope(principal), updated)
	s.record(ctx, principal, "knowledge.base.update", current.ID, updated.Name, err)
	return result, err
}

func (s *Service) DeleteBase(ctx context.Context, principal domainidentity.Principal, baseID string) error {
	if err := s.authorize(ctx, principal, PermKnowledgeManage); err != nil {
		return err
	}
	item, err := s.repo.GetBase(ctx, principalScope(principal), strings.TrimSpace(baseID))
	if err != nil {
		return err
	}
	err = s.repo.DeleteBase(ctx, principalScope(principal), item.ID)
	s.record(ctx, principal, "knowledge.base.delete", item.ID, item.Name, err)
	return err
}

func (s *Service) ListSources(ctx context.Context, principal domainidentity.Principal, baseID string) ([]domainknowledge.Source, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return nil, err
	}
	return s.repo.ListSources(ctx, principalScope(principal), strings.TrimSpace(baseID))
}

func (s *Service) CreateSource(ctx context.Context, principal domainidentity.Principal, baseID string, input domainknowledge.SourceInput) (domainknowledge.Source, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeManage); err != nil {
		return domainknowledge.Source{}, err
	}
	if _, err := s.repo.GetBase(ctx, principalScope(principal), strings.TrimSpace(baseID)); err != nil {
		return domainknowledge.Source{}, err
	}
	item, err := normalizeSourceInput(baseID, input, s.now())
	if err != nil {
		return domainknowledge.Source{}, err
	}
	if item.Kind != domainknowledge.SourceKindInline {
		if s.connectorValidator == nil {
			return domainknowledge.Source{}, fmt.Errorf("%w: connector validator is not configured", apperrors.ErrUnsupportedOperation)
		}
		if _, err := s.connectorValidator.Validate(ctx, connectorInputFromSource(item)); err != nil {
			return domainknowledge.Source{}, err
		}
	}
	created, err := s.repo.CreateSource(ctx, principalScope(principal), item)
	s.record(ctx, principal, "knowledge.source.create", item.ID, item.Name, err)
	return created, err
}

func (s *Service) ListDocuments(ctx context.Context, principal domainidentity.Principal, baseID string, limit int) ([]domainknowledge.Document, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return nil, err
	}
	return s.repo.ListDocuments(ctx, principalScope(principal), strings.TrimSpace(baseID), boundedLimit(limit))
}

func (s *Service) ListSyncRuns(ctx context.Context, principal domainidentity.Principal, baseID string, limit int) ([]domainknowledge.SyncRun, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return nil, err
	}
	return s.repo.ListSyncRuns(ctx, principalScope(principal), strings.TrimSpace(baseID), boundedLimit(limit))
}

func (s *Service) ListIndexRevisions(ctx context.Context, principal domainidentity.Principal, baseID string, limit int) ([]domainknowledge.IndexRevision, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return nil, err
	}
	return s.repo.ListIndexRevisions(ctx, principalScope(principal), strings.TrimSpace(baseID), boundedLimit(limit))
}

func (s *Service) Search(ctx context.Context, principal domainidentity.Principal, request domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	started := s.now()
	if err := s.authorize(ctx, principal, PermKnowledgeView); err != nil {
		return domainknowledge.SearchResult{}, err
	}
	request.Query = strings.TrimSpace(request.Query)
	request.KnowledgeBaseIDs = uniqueStrings(request.KnowledgeBaseIDs)
	if request.Query == "" || len(request.KnowledgeBaseIDs) == 0 {
		return domainknowledge.SearchResult{}, fmt.Errorf("%w: query and knowledgeBaseIds are required", apperrors.ErrInvalidArgument)
	}
	topK := min(max(request.TopK, 5), 50)
	candidateLimit := min(max(topK*10, 50), maxCandidates)
	chunks, err := s.repo.ListAuthorizedChunks(ctx, principalScope(principal), request, candidateLimit)
	if err != nil {
		return domainknowledge.SearchResult{}, err
	}
	lexical := s.lexical.Score(request.Query, chunks)
	vector, err := s.vector.Score(ctx, principal, request.Query, chunks)
	if err != nil {
		vector = make([]float64, len(chunks))
	}
	hits := buildHits(chunks, lexical, vector)
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > topK*2 {
		hits = hits[:topK*2]
	}
	if s.reranker != nil && len(hits) > 0 {
		if reranked, rerankErr := s.reranker.Rerank(ctx, principal, request.Query, hits); rerankErr == nil {
			hits = reranked
		}
	}
	if len(hits) > topK {
		hits = hits[:topK]
	}
	citations := make([]domainknowledge.Citation, 0, len(hits))
	for _, hit := range hits {
		citations = append(citations, hit.Citation)
	}
	traceID := requestctx.FromContext(ctx).TraceID
	if traceID == "" {
		traceID = uuid.NewString()
	}
	return domainknowledge.SearchResult{Query: request.Query, Hits: hits, Citations: citations, CandidateN: len(chunks), TimingMS: s.now().Sub(started).Milliseconds(), NoAnswer: len(hits) == 0 || hits[0].Score < 0.05, TraceID: traceID}, nil
}

func buildHits(chunks []domainknowledge.Chunk, lexical, vector []float64) []domainknowledge.SearchHit {
	hits := make([]domainknowledge.SearchHit, 0, len(chunks))
	for i, chunk := range chunks {
		lexicalScore, vectorScore := scoreAt(lexical, i), scoreAt(vector, i)
		score := 0.45*lexicalScore + 0.55*vectorScore
		citation := domainknowledge.Citation{ID: "citation:" + chunk.ID, KnowledgeBaseID: chunk.KnowledgeBaseID, DocumentID: chunk.DocumentID, DocumentTitle: chunk.DocumentTitle, ChunkID: chunk.ID, Location: chunk.Location, URI: chunk.Location.URI, Score: score, ContentHash: chunk.ContentHash}
		hits = append(hits, domainknowledge.SearchHit{ChunkID: chunk.ID, DocumentID: chunk.DocumentID, KnowledgeBaseID: chunk.KnowledgeBaseID, Title: chunk.DocumentTitle, Content: chunk.Content, Score: score, LexicalScore: lexicalScore, VectorScore: vectorScore, Citation: citation})
	}
	return hits
}

func scoreAt(scores []float64, index int) float64 {
	if index >= 0 && index < len(scores) {
		return scores[index]
	}
	return 0
}

func normalizeBaseInput(input domainknowledge.BaseInput, principal domainidentity.Principal, now time.Time) (domainknowledge.KnowledgeBase, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 160 {
		return domainknowledge.KnowledgeBase{}, fmt.Errorf("%w: knowledge base name is required and must not exceed 160 characters", apperrors.ErrInvalidArgument)
	}
	input.Scope = normalizeAccessScope(input.Scope, principal.UserID)
	policy := input.RetrievalPolicy
	if policy.DefaultTopK <= 0 {
		policy.DefaultTopK = 5
	}
	if policy.MaxTopK <= 0 {
		policy.MaxTopK = 20
	}
	if policy.MaxTopK > 50 {
		policy.MaxTopK = 50
	}
	if policy.LexicalWeight <= 0 && policy.VectorWeight <= 0 {
		policy.LexicalWeight, policy.VectorWeight = 0.45, 0.55
	}
	return domainknowledge.KnowledgeBase{ID: uuid.NewString(), TenantID: strings.TrimSpace(input.TenantID), WorkspaceID: strings.TrimSpace(input.WorkspaceID), Name: input.Name, Description: strings.TrimSpace(input.Description), Status: domainknowledge.BaseStatusActive, OwnerID: principal.UserID, Scope: input.Scope, RetrievalPolicy: policy, CreatedAt: now, UpdatedAt: now}, nil
}

func normalizeSourceInput(baseID string, input domainknowledge.SourceInput, now time.Time) (domainknowledge.Source, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || strings.TrimSpace(baseID) == "" {
		return domainknowledge.Source{}, fmt.Errorf("%w: source name and knowledge base are required", apperrors.ErrInvalidArgument)
	}
	switch input.Kind {
	case domainknowledge.SourceKindInline, domainknowledge.SourceKindHTTP, domainknowledge.SourceKindGit, domainknowledge.SourceKindObject:
	default:
		return domainknowledge.Source{}, fmt.Errorf("%w: unsupported source kind", apperrors.ErrInvalidArgument)
	}
	if input.Kind != domainknowledge.SourceKindInline && !strings.HasPrefix(strings.TrimSpace(input.ConfigRef), "secret:") {
		return domainknowledge.Source{}, fmt.Errorf("%w: non-inline sources require a secret: configRef", apperrors.ErrInvalidArgument)
	}
	if hasSecretConfigKey(input.Config) {
		return domainknowledge.Source{}, fmt.Errorf("%w: credentials must use configRef and cannot be stored in source config", apperrors.ErrInvalidArgument)
	}
	if input.SyncPolicy.Mode == "" {
		input.SyncPolicy.Mode = "manual"
	}
	return domainknowledge.Source{ID: uuid.NewString(), KnowledgeBaseID: strings.TrimSpace(baseID), Name: input.Name, Kind: input.Kind, ConfigRef: strings.TrimSpace(input.ConfigRef), Config: input.Config, SyncPolicy: input.SyncPolicy, Status: domainknowledge.SourceStatusPending, CreatedAt: now, UpdatedAt: now}, nil
}

func normalizeAccessScope(scope domainknowledge.AccessScope, ownerID string) domainknowledge.AccessScope {
	scope.Visibility = strings.ToLower(strings.TrimSpace(scope.Visibility))
	if scope.Visibility != "public" && scope.Visibility != "restricted" {
		scope.Visibility = "private"
	}
	scope.Users, scope.Roles, scope.Teams, scope.Projects = uniqueStrings(scope.Users), uniqueStrings(scope.Roles), uniqueStrings(scope.Teams), uniqueStrings(scope.Projects)
	if scope.Visibility == "private" && !contains(scope.Users, ownerID) {
		scope.Users = append(scope.Users, ownerID)
	}
	return scope
}

func principalScope(principal domainidentity.Principal) domainknowledge.PrincipalScope {
	return domainknowledge.PrincipalScope{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects}
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return min(limit, maxListItems)
}

func uniqueStrings(values []string) []string {
	seen, out := map[string]struct{}{}, make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasSecretConfigKey(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			switch normalized {
			case "secret", "password", "token", "apikey", "authorization", "credential":
				return true
			}
			if hasSecretConfigKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if hasSecretConfigKey(child) {
				return true
			}
		}
	}
	return false
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func (s *Service) record(ctx context.Context, principal domainidentity.Principal, operationType, targetID, label string, operationErr error) {
	if s.operations == nil {
		return
	}
	result, summary := "success", operationType+" succeeded"
	if operationErr != nil {
		result, summary = "failure", operationType+" failed"
	}
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operationType, map[string]any{"module": "ai", "resourceKind": "Knowledge", "targetId": targetID, "targetLabel": label}, result, summary, nil))
}

func IsNotFound(err error) bool {
	return errors.Is(err, domainknowledge.ErrBaseNotFound) || errors.Is(err, domainknowledge.ErrSourceNotFound)
}
