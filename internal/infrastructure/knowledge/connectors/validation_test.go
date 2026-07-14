package connectors

import (
	"context"
	"errors"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestValidatorExternalConnectorSecurityBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input domainknowledge.ConnectorInput
	}{
		{
			name: "HTTP private IP",
			input: domainknowledge.ConnectorInput{Kind: domainknowledge.SourceKindHTTP, SecretRef: "secret:http", Config: map[string]any{
				"url": "https://127.0.0.1/runbook", "allowedHosts": []string{"127.0.0.1"}, "maxBytes": 1024,
			}},
		},
		{
			name: "Git traversal",
			input: domainknowledge.ConnectorInput{Kind: domainknowledge.SourceKindGit, SecretRef: "secret:git", Config: map[string]any{
				"repositoryUrl": "https://git.example.com/team/runbooks.git", "allowedHosts": []string{"git.example.com"},
				"branch": "main", "path": "../secrets", "depth": 1, "maxBytes": 1024,
			}},
		},
		{
			name: "Object bucket bounds",
			input: domainknowledge.ConnectorInput{Kind: domainknowledge.SourceKindObject, SecretRef: "secret:object", Config: map[string]any{
				"endpoint": "https://objects.example.com", "allowedHosts": []string{"objects.example.com"},
				"bucket": "A", "prefix": "runbooks", "maxObjects": 10, "maxBytes": 1024,
			}},
		},
		{
			name: "Inline credential",
			input: domainknowledge.ConnectorInput{Kind: domainknowledge.SourceKindHTTP, SecretRef: "secret:http", Config: map[string]any{
				"url": "https://docs.example.com", "allowedHosts": []string{"docs.example.com"}, "maxBytes": 1024,
				"token": "must-not-be-stored",
			}},
		},
		{
			name: "Non-standard port",
			input: domainknowledge.ConnectorInput{Kind: domainknowledge.SourceKindHTTP, SecretRef: "secret:http", Config: map[string]any{
				"url": "https://docs.example.com:8443/runbook", "allowedHosts": []string{"docs.example.com"}, "maxBytes": 1024,
			}},
		},
		{
			name: "Encoded URL traversal",
			input: domainknowledge.ConnectorInput{Kind: domainknowledge.SourceKindHTTP, SecretRef: "secret:http", Config: map[string]any{
				"url": "https://docs.example.com/%2e%2e/private", "allowedHosts": []string{"docs.example.com"}, "maxBytes": 1024,
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewValidator().Validate(context.Background(), test.input)
			if !errors.Is(err, domainknowledge.ErrInvalidInput) {
				t.Fatalf("Validate() error = %v, want invalid input", err)
			}
		})
	}
}

func TestValidatorAcceptsBoundedExternalConnectors(t *testing.T) {
	t.Parallel()
	tests := []domainknowledge.ConnectorInput{
		{Kind: domainknowledge.SourceKindHTTP, SecretRef: "secret:http", Config: map[string]any{
			"url": "https://docs.example.com/runbooks", "allowedHosts": []string{"docs.example.com"}, "maxBytes": 1024,
		}},
		{Kind: domainknowledge.SourceKindGit, SecretRef: "secret:git", Config: map[string]any{
			"repositoryUrl": "https://git.example.com/team/runbooks.git", "allowedHosts": []string{"git.example.com"},
			"branch": "main", "path": "docs/runbooks", "depth": 1, "maxBytes": 4096,
		}},
		{Kind: domainknowledge.SourceKindObject, SecretRef: "secret:object", Config: map[string]any{
			"endpoint": "https://objects.example.com", "allowedHosts": []string{"objects.example.com"},
			"bucket": "team-runbooks", "prefix": "production", "maxObjects": 100, "maxBytes": 4096,
		}},
	}
	for _, input := range tests {
		result, err := NewValidator().Validate(context.Background(), input)
		if err != nil {
			t.Fatalf("Validate(%s) error = %v", input.Kind, err)
		}
		if !result.Valid || result.ConfigHash == "" || result.SecretRef != input.SecretRef {
			t.Fatalf("Validate(%s) = %#v", input.Kind, result)
		}
	}
}

type fetcherStub struct{}

func (fetcherStub) Fetch(context.Context, FetchRequest) (FetchResult, error) {
	return FetchResult{Documents: []domainknowledge.SourceDocument{{ExternalID: "doc-1", Content: "bounded"}}, Cursor: "v1:cursor"}, nil
}

func TestExternalAdapterUsesValidatedAbstractFetcher(t *testing.T) {
	t.Parallel()
	loader := ProductionLoader(fetcherStub{}, NewValidator())
	documents, cursor, err := loader.Load(context.Background(), domainidentity.Principal{}, domainknowledge.Source{
		Kind: domainknowledge.SourceKindHTTP, ConfigRef: "secret:http", Config: map[string]any{
			"url": "https://docs.example.com/runbooks", "allowedHosts": []string{"docs.example.com"}, "maxBytes": 1024,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 || cursor != "v1:cursor" {
		t.Fatalf("Load() documents=%#v cursor=%q", documents, cursor)
	}
}
