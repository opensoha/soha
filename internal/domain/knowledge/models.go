package knowledge

import "time"

type AccessScope struct {
	Visibility string   `json:"visibility"`
	Users      []string `json:"users,omitempty"`
	Roles      []string `json:"roles,omitempty"`
	Teams      []string `json:"teams,omitempty"`
	Projects   []string `json:"projects,omitempty"`
}

type RetrievalPolicy struct {
	DefaultTopK   int     `json:"defaultTopK"`
	MaxTopK       int     `json:"maxTopK"`
	LexicalWeight float64 `json:"lexicalWeight"`
	VectorWeight  float64 `json:"vectorWeight"`
	MinScore      float64 `json:"minScore"`
}

type KnowledgeBase struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenantId,omitempty"`
	WorkspaceID     string          `json:"workspaceId,omitempty"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	Status          BaseStatus      `json:"status"`
	OwnerID         string          `json:"ownerId"`
	Scope           AccessScope     `json:"scope"`
	RetrievalPolicy RetrievalPolicy `json:"retrievalPolicy"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type BaseInput struct {
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	TenantID        string          `json:"tenantId,omitempty"`
	WorkspaceID     string          `json:"workspaceId,omitempty"`
	Scope           AccessScope     `json:"scope"`
	RetrievalPolicy RetrievalPolicy `json:"retrievalPolicy"`
}

type SourceKind string

const (
	SourceKindInline SourceKind = "inline"
	SourceKindHTTP   SourceKind = "http"
	SourceKindGit    SourceKind = "git"
	SourceKindObject SourceKind = "object"
)

type SyncPolicy struct {
	Mode     string `json:"mode"`
	Schedule string `json:"schedule,omitempty"`
}

type Source struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	Name            string         `json:"name"`
	Kind            SourceKind     `json:"kind"`
	ConfigRef       string         `json:"configRef,omitempty"`
	Config          map[string]any `json:"-"`
	SyncPolicy      SyncPolicy     `json:"syncPolicy"`
	Cursor          string         `json:"cursor,omitempty"`
	Status          SourceStatus   `json:"status"`
	LastError       string         `json:"lastError,omitempty"`
	LastSyncedAt    *time.Time     `json:"lastSyncedAt,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type SourceInput struct {
	Name       string         `json:"name"`
	Kind       SourceKind     `json:"kind"`
	ConfigRef  string         `json:"configRef,omitempty"`
	Config     map[string]any `json:"config,omitempty"`
	SyncPolicy SyncPolicy     `json:"syncPolicy"`
}

type SourceDocument struct {
	ExternalID string      `json:"externalId"`
	Title      string      `json:"title"`
	Content    string      `json:"content"`
	URI        string      `json:"uri,omitempty"`
	Version    string      `json:"version,omitempty"`
	ACL        AccessScope `json:"acl"`
}

type Document struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	SourceID        string         `json:"sourceId"`
	ExternalID      string         `json:"externalId"`
	Title           string         `json:"title"`
	URI             string         `json:"uri,omitempty"`
	Version         string         `json:"version"`
	ContentHash     string         `json:"contentHash"`
	ACL             AccessScope    `json:"acl"`
	Status          DocumentStatus `json:"status"`
	ChunkCount      int            `json:"chunkCount"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type SourceLocation struct {
	URI       string `json:"uri,omitempty"`
	StartByte int    `json:"startByte,omitempty"`
	EndByte   int    `json:"endByte,omitempty"`
}

type Chunk struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	DocumentID      string         `json:"documentId"`
	DocumentTitle   string         `json:"documentTitle"`
	Ordinal         int            `json:"ordinal"`
	Content         string         `json:"content"`
	ContentHash     string         `json:"contentHash"`
	Location        SourceLocation `json:"location"`
	TokenCount      int            `json:"tokenCount"`
	ACL             AccessScope    `json:"acl"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type IndexRevision struct {
	ID              string      `json:"id"`
	KnowledgeBaseID string      `json:"knowledgeBaseId"`
	Revision        int         `json:"revision"`
	EmbeddingModel  string      `json:"embeddingModel,omitempty"`
	ChunkerVersion  string      `json:"chunkerVersion"`
	DocumentCount   int         `json:"documentCount"`
	ChunkCount      int         `json:"chunkCount"`
	Status          IndexStatus `json:"status"`
	CreatedAt       time.Time   `json:"createdAt"`
	ActivatedAt     *time.Time  `json:"activatedAt,omitempty"`
}

type SyncRun struct {
	ID              string     `json:"id"`
	KnowledgeBaseID string     `json:"knowledgeBaseId"`
	SourceID        string     `json:"sourceId"`
	Status          RunStatus  `json:"status"`
	DocumentsSeen   int        `json:"documentsSeen"`
	DocumentsStored int        `json:"documentsStored"`
	ChunksStored    int        `json:"chunksStored"`
	Error           string     `json:"error,omitempty"`
	StartedAt       time.Time  `json:"startedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

type PrincipalScope struct {
	UserID   string
	Roles    []string
	Teams    []string
	Projects []string
}

type SearchFilters struct {
	SourceIDs   []string `json:"sourceIds,omitempty"`
	DocumentIDs []string `json:"documentIds,omitempty"`
}

type SearchRequest struct {
	KnowledgeBaseIDs []string      `json:"knowledgeBaseIds"`
	Query            string        `json:"query"`
	TopK             int           `json:"topK,omitempty"`
	Filters          SearchFilters `json:"filters,omitempty"`
}

type Citation struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	DocumentID      string         `json:"documentId"`
	DocumentTitle   string         `json:"documentTitle"`
	ChunkID         string         `json:"chunkId"`
	Location        SourceLocation `json:"location"`
	URI             string         `json:"uri,omitempty"`
	Score           float64        `json:"score"`
	ContentHash     string         `json:"contentHash"`
}

type SearchHit struct {
	ChunkID         string   `json:"chunkId"`
	DocumentID      string   `json:"documentId"`
	KnowledgeBaseID string   `json:"knowledgeBaseId"`
	Title           string   `json:"title"`
	Content         string   `json:"content"`
	Score           float64  `json:"score"`
	LexicalScore    float64  `json:"lexicalScore"`
	VectorScore     float64  `json:"vectorScore"`
	Citation        Citation `json:"citation"`
}

type SearchResult struct {
	Query      string      `json:"query"`
	Hits       []SearchHit `json:"hits"`
	Citations  []Citation  `json:"citations"`
	CandidateN int         `json:"candidateCount"`
	TimingMS   int64       `json:"timingMs"`
	NoAnswer   bool        `json:"noAnswer"`
	TraceID    string      `json:"traceId"`
}
