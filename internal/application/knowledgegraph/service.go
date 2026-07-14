package knowledgegraph

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("knowledge graph revision not found")

type Entity struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	SourceRefs  []string `json:"sourceRefs"`
	ContentHash string   `json:"contentHash"`
}

type Relation struct {
	ID           string   `json:"id"`
	FromEntityID string   `json:"fromEntityId"`
	ToEntityID   string   `json:"toEntityId"`
	Kind         string   `json:"kind"`
	SourceRefs   []string `json:"sourceRefs"`
	Confidence   float64  `json:"confidence"`
}

type Community struct {
	ID         string   `json:"id"`
	EntityIDs  []string `json:"entityIds"`
	Summary    string   `json:"summary"`
	SourceRefs []string `json:"sourceRefs"`
}

type Revision struct {
	ID              string      `json:"id"`
	KnowledgeBaseID string      `json:"knowledgeBaseId"`
	SourceIndexRef  string      `json:"sourceIndexRef"`
	ExtractorVer    string      `json:"extractorVersion"`
	Status          string      `json:"status"`
	Entities        []Entity    `json:"entities"`
	Relations       []Relation  `json:"relations"`
	Communities     []Community `json:"communities"`
	CreatedAt       time.Time   `json:"createdAt"`
	PublishedAt     *time.Time  `json:"publishedAt,omitempty"`
}

type QueryResult struct {
	RevisionID string      `json:"revisionId"`
	Mode       string      `json:"mode"`
	Entities   []Entity    `json:"entities"`
	Relations  []Relation  `json:"relations"`
	Summaries  []Community `json:"summaries"`
	NoAnswer   bool        `json:"noAnswer"`
}

type Store interface {
	Put(context.Context, Revision) error
	Get(context.Context, string) (Revision, error)
	List(context.Context, string) ([]Revision, error)
	Publish(context.Context, string, time.Time) (Revision, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("knowledge graph store is required")
	}
	return &Service{store: store, now: time.Now}, nil
}

func (s *Service) PutRevision(ctx context.Context, revision Revision) error {
	revision.ID = strings.TrimSpace(revision.ID)
	revision.KnowledgeBaseID = strings.TrimSpace(revision.KnowledgeBaseID)
	revision.SourceIndexRef = strings.TrimSpace(revision.SourceIndexRef)
	revision.ExtractorVer = strings.TrimSpace(revision.ExtractorVer)
	if revision.ID == "" || revision.KnowledgeBaseID == "" || revision.SourceIndexRef == "" || revision.ExtractorVer == "" {
		return fmt.Errorf("knowledge graph revision identity is required")
	}
	if len(revision.Entities) > 50_000 || len(revision.Relations) > 200_000 || len(revision.Communities) > 5_000 {
		return fmt.Errorf("knowledge graph revision exceeds bounded size")
	}
	entityIDs := make(map[string]struct{}, len(revision.Entities))
	for i := range revision.Entities {
		entity := &revision.Entities[i]
		entity.ID = strings.TrimSpace(entity.ID)
		entity.Name = strings.TrimSpace(entity.Name)
		entity.SourceRefs = normalizeRefs(entity.SourceRefs)
		if entity.ID == "" || entity.Name == "" || len(entity.SourceRefs) == 0 {
			return fmt.Errorf("knowledge graph entity identity and provenance are required")
		}
		if _, exists := entityIDs[entity.ID]; exists {
			return fmt.Errorf("duplicate knowledge graph entity %q", entity.ID)
		}
		entityIDs[entity.ID] = struct{}{}
	}
	for i := range revision.Relations {
		relation := &revision.Relations[i]
		relation.SourceRefs = normalizeRefs(relation.SourceRefs)
		_, hasFrom := entityIDs[relation.FromEntityID]
		_, hasTo := entityIDs[relation.ToEntityID]
		if relation.ID == "" || !hasFrom || !hasTo || len(relation.SourceRefs) == 0 || relation.Confidence < 0 || relation.Confidence > 1 {
			return fmt.Errorf("invalid knowledge graph relation")
		}
	}
	revision.Status = "verified"
	revision.CreatedAt = s.now().UTC()
	return s.store.Put(ctx, cloneRevision(revision))
}

func (s *Service) Publish(ctx context.Context, id string) (Revision, error) {
	revision, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return Revision{}, err
	}
	if revision.Status != "verified" {
		return Revision{}, fmt.Errorf("knowledge graph revision is not verified")
	}
	return s.store.Publish(ctx, revision.ID, s.now().UTC())
}

func (s *Service) GetRevision(ctx context.Context, id string) (Revision, error) {
	revision, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return Revision{}, err
	}
	return cloneRevision(revision), nil
}

func (s *Service) List(ctx context.Context, knowledgeBaseID string) ([]Revision, error) {
	return s.store.List(ctx, strings.TrimSpace(knowledgeBaseID))
}

func (s *Service) Query(ctx context.Context, revisionID, query, mode string, limit int) (QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return QueryResult{}, err
	}
	revision, err := s.store.Get(ctx, strings.TrimSpace(revisionID))
	if err != nil {
		return QueryResult{}, err
	}
	if revision.Status != "active" {
		return QueryResult{}, fmt.Errorf("knowledge graph revision is not active")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return QueryResult{}, fmt.Errorf("knowledge graph query is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	result := QueryResult{RevisionID: revision.ID, Mode: mode, Entities: []Entity{}, Relations: []Relation{}, Summaries: []Community{}}
	if mode == "global" {
		for _, community := range revision.Communities {
			if strings.Contains(strings.ToLower(community.Summary), query) {
				result.Summaries = append(result.Summaries, community)
			}
			if len(result.Summaries) == limit {
				break
			}
		}
	} else {
		matched := map[string]struct{}{}
		for _, entity := range revision.Entities {
			if strings.Contains(strings.ToLower(entity.Name), query) {
				result.Entities = append(result.Entities, entity)
				matched[entity.ID] = struct{}{}
			}
			if len(result.Entities) == limit {
				break
			}
		}
		for _, relation := range revision.Relations {
			_, from := matched[relation.FromEntityID]
			_, to := matched[relation.ToEntityID]
			if from || to {
				result.Relations = append(result.Relations, relation)
			}
		}
	}
	result.NoAnswer = len(result.Entities) == 0 && len(result.Summaries) == 0
	return result, nil
}

type MemoryStore struct {
	mu        sync.RWMutex
	revisions map[string]Revision
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{revisions: map[string]Revision{}} }

func (s *MemoryStore) Put(_ context.Context, revision Revision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revisions[revision.ID] = cloneRevision(revision)
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	revision, ok := s.revisions[id]
	if !ok {
		return Revision{}, ErrNotFound
	}
	return cloneRevision(revision), nil
}

func (s *MemoryStore) List(_ context.Context, knowledgeBaseID string) ([]Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Revision, 0)
	for _, revision := range s.revisions {
		if revision.KnowledgeBaseID == knowledgeBaseID {
			items = append(items, cloneRevision(revision))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *MemoryStore) Publish(_ context.Context, id string, now time.Time) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision, ok := s.revisions[id]
	if !ok {
		return Revision{}, ErrNotFound
	}
	for otherID, other := range s.revisions {
		if other.KnowledgeBaseID == revision.KnowledgeBaseID && other.Status == "active" {
			other.Status = "superseded"
			s.revisions[otherID] = other
		}
	}
	revision.Status = "active"
	revision.PublishedAt = &now
	s.revisions[id] = revision
	return cloneRevision(revision), nil
}

func normalizeRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref = strings.TrimSpace(ref); ref != "" {
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func cloneRevision(revision Revision) Revision {
	revision.Entities = slices.Clone(revision.Entities)
	for i := range revision.Entities {
		revision.Entities[i].SourceRefs = slices.Clone(revision.Entities[i].SourceRefs)
	}
	revision.Relations = slices.Clone(revision.Relations)
	for i := range revision.Relations {
		revision.Relations[i].SourceRefs = slices.Clone(revision.Relations[i].SourceRefs)
	}
	revision.Communities = slices.Clone(revision.Communities)
	for i := range revision.Communities {
		revision.Communities[i].EntityIDs = slices.Clone(revision.Communities[i].EntityIDs)
		revision.Communities[i].SourceRefs = slices.Clone(revision.Communities[i].SourceRefs)
	}
	return revision
}
