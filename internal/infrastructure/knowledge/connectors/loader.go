package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const (
	maxInlineDocuments = 1000
	maxInlineBytes     = 8 << 20
)

// Loader intentionally supports only bounded inline documents locally. Network,
// Git, and object-store sources must be registered as governed connector adapters.
type Loader struct {
	adapters map[domainknowledge.SourceKind]Adapter
}

type Adapter interface {
	Load(context.Context, domainidentity.Principal, domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error)
}

type FetchRequest struct {
	Kind                domainknowledge.SourceKind
	Principal           domainidentity.Principal
	SecretRef           string
	Config              map[string]any
	Cursor              string
	AllowedHosts        []string
	DenyPrivateNetworks bool
	MaxRedirects        int
	MaxItems            int
	MaxBytes            int
}

type FetchResult struct {
	Documents []domainknowledge.SourceDocument
	Cursor    string
}

type Fetcher interface {
	Fetch(context.Context, FetchRequest) (FetchResult, error)
}

type UnavailableFetcher struct{}

func (UnavailableFetcher) Fetch(context.Context, FetchRequest) (FetchResult, error) {
	return FetchResult{}, fmt.Errorf("%w: external connector fetcher is not configured", domainknowledge.ErrSourceUnavailable)
}

func NewLoader(adapters map[domainknowledge.SourceKind]Adapter) *Loader {
	copy := make(map[domainknowledge.SourceKind]Adapter, len(adapters))
	for kind, adapter := range adapters {
		if adapter != nil {
			copy[kind] = adapter
		}
	}
	return &Loader{adapters: copy}
}

func DefaultLoader() *Loader {
	return NewLoader(map[domainknowledge.SourceKind]Adapter{domainknowledge.SourceKindInline: InlineAdapter{}})
}

func ProductionLoader(fetcher Fetcher, validator *Validator) *Loader {
	adapters := map[domainknowledge.SourceKind]Adapter{domainknowledge.SourceKindInline: InlineAdapter{}}
	if fetcher != nil {
		for _, kind := range []domainknowledge.SourceKind{
			domainknowledge.SourceKindHTTP,
			domainknowledge.SourceKindGit,
			domainknowledge.SourceKindObject,
		} {
			adapters[kind] = ExternalAdapter{kind: kind, fetcher: fetcher, validator: validator}
		}
	}
	return NewLoader(adapters)
}

func (l *Loader) Load(ctx context.Context, principal domainidentity.Principal, source domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error) {
	adapter, ok := l.adapters[source.Kind]
	if !ok {
		return nil, "", fmt.Errorf("%w: no connector registered for %s", domainknowledge.ErrSourceUnavailable, source.Kind)
	}
	return adapter.Load(ctx, principal, source)
}

type InlineAdapter struct{}

func (InlineAdapter) Load(ctx context.Context, _ domainidentity.Principal, source domainknowledge.Source) ([]domainknowledge.SourceDocument, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	payload, err := json.Marshal(source.Config["documents"])
	if err != nil {
		return nil, "", fmt.Errorf("decode inline documents: %w", err)
	}
	var documents []domainknowledge.SourceDocument
	if err := json.Unmarshal(payload, &documents); err != nil {
		return nil, "", fmt.Errorf("decode inline documents: %w", err)
	}
	if len(documents) > maxInlineDocuments {
		return nil, "", fmt.Errorf("%w: inline document count exceeds %d", domainknowledge.ErrInvalidInput, maxInlineDocuments)
	}
	total := 0
	for i := range documents {
		documents[i].ExternalID = strings.TrimSpace(documents[i].ExternalID)
		documents[i].Content = strings.TrimSpace(documents[i].Content)
		total += len(documents[i].Content)
		if documents[i].ExternalID == "" || documents[i].Content == "" {
			return nil, "", fmt.Errorf("%w: inline document externalId and content are required", domainknowledge.ErrInvalidInput)
		}
		if total > maxInlineBytes {
			return nil, "", fmt.Errorf("%w: inline content exceeds %d bytes", domainknowledge.ErrInvalidInput, maxInlineBytes)
		}
	}
	return documents, source.Cursor, nil
}

type ExternalAdapter struct {
	kind      domainknowledge.SourceKind
	fetcher   Fetcher
	validator *Validator
}

func (a ExternalAdapter) Load(
	ctx context.Context,
	principal domainidentity.Principal,
	source domainknowledge.Source,
) ([]domainknowledge.SourceDocument, string, error) {
	if source.Kind != a.kind || a.fetcher == nil || a.validator == nil {
		return nil, "", fmt.Errorf("%w: external connector adapter is unavailable", domainknowledge.ErrSourceUnavailable)
	}
	if _, err := a.validator.Validate(ctx, domainknowledge.ConnectorInput{
		KnowledgeBaseID: source.KnowledgeBaseID,
		Name:            source.Name,
		Kind:            source.Kind,
		SecretRef:       source.ConfigRef,
		Config:          source.Config,
		SyncPolicy:      source.SyncPolicy,
	}); err != nil {
		return nil, "", err
	}
	maxItems, _ := intConfig(source.Config, "maxObjects")
	if maxItems == 0 {
		maxItems = maxInlineDocuments
	}
	maxBytes, _ := intConfig(source.Config, "maxBytes")
	result, err := a.fetcher.Fetch(ctx, FetchRequest{
		Kind:                source.Kind,
		Principal:           principal,
		SecretRef:           source.ConfigRef,
		Config:              source.Config,
		Cursor:              source.Cursor,
		AllowedHosts:        stringSliceConfig(source.Config, "allowedHosts"),
		DenyPrivateNetworks: true,
		MaxRedirects:        3,
		MaxItems:            maxItems,
		MaxBytes:            maxBytes,
	})
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s connector: %w", source.Kind, err)
	}
	if len(result.Documents) > maxItems {
		return nil, "", fmt.Errorf("%w: connector returned too many documents", domainknowledge.ErrRetrievalExhausted)
	}
	total := 0
	for _, document := range result.Documents {
		total += len(document.Content)
		if total > maxBytes {
			return nil, "", fmt.Errorf("%w: connector content exceeds maxBytes", domainknowledge.ErrRetrievalExhausted)
		}
	}
	if len(result.Cursor) > 8192 {
		return nil, "", fmt.Errorf("%w: connector cursor exceeds 8 KiB", domainknowledge.ErrInvalidInput)
	}
	return result.Documents, result.Cursor, nil
}
