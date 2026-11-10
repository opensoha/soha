package aigatewayrelay

import (
	"net/http"
	"slices"
	"testing"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
)

func TestRelayEndpointURLUsesOpenAIPathsForFirstClassOpenAICompatibleProviders(t *testing.T) {
	for _, tt := range []struct {
		name         string
		baseURL      string
		providerKind string
		endpoint     string
		want         string
	}{
		{
			name:         "deepseek chat appends v1",
			baseURL:      "https://api.deepseek.com",
			providerKind: "deepseek",
			endpoint:     "chat/completions",
			want:         "https://api.deepseek.com/v1/chat/completions",
		},
		{
			name:         "qwen compatible mode keeps v1 suffix",
			baseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
			providerKind: "qwen",
			endpoint:     "models",
			want:         "https://dashscope.aliyuncs.com/compatible-mode/v1/models",
		},
		{
			name:         "openrouter api v1 keeps v1 suffix",
			baseURL:      "https://openrouter.ai/api/v1",
			providerKind: "openrouter",
			endpoint:     "responses",
			want:         "https://openrouter.ai/api/v1/responses",
		},
		{
			name:         "deepseek image generation appends v1",
			baseURL:      "https://api.deepseek.com",
			providerKind: "deepseek",
			endpoint:     "images/generations",
			want:         "https://api.deepseek.com/v1/images/generations",
		},
		{
			name:         "openai compatible image edits appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "images/edits",
			want:         "https://api.openai.com/v1/images/edits",
		},
		{
			name:         "openai compatible image variations appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "images/variations",
			want:         "https://api.openai.com/v1/images/variations",
		},
		{
			name:         "openai compatible audio speech appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "audio/speech",
			want:         "https://api.openai.com/v1/audio/speech",
		},
		{
			name:         "openai compatible audio transcription appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "audio/transcriptions",
			want:         "https://api.openai.com/v1/audio/transcriptions",
		},
		{
			name:         "openai compatible audio translation appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "audio/translations",
			want:         "https://api.openai.com/v1/audio/translations",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURL(tt.baseURL, tt.providerKind, tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayEndpointURLUsesAzureOpenAIPaths(t *testing.T) {
	for _, tt := range azureOpenAIPathCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURLForUpstream(tt.upstream, tt.route, "azure-openai", tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURLForUpstream returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURLForUpstream = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayEndpointURLUsesGeminiNativePaths(t *testing.T) {
	for _, tt := range []struct {
		name     string
		upstream domainaigateway.LLMUpstream
		route    domainaigateway.LLMModelRoute
		endpoint string
		want     string
	}{
		{
			name: "generate content appends v1beta",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com",
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "gemini-2.0-flash",
			},
			endpoint: "generateContent",
			want:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent",
		},
		{
			name: "stream generate content keeps v1beta suffix and requests sse",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "models/gemini-2.0-flash",
			},
			endpoint: "streamGenerateContent",
			want:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse",
		},
		{
			name: "metadata version override",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com",
				Metadata: map[string]any{
					"gemini": map[string]any{"apiVersion": "v1"},
				},
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "gemini-2.0-flash",
			},
			endpoint: "models",
			want:     "https://generativelanguage.googleapis.com/v1/models",
		},
		{
			name: "interactions appends v1beta resource path",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com",
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "gemini-2.5-flash-image",
			},
			endpoint: "interactions",
			want:     "https://generativelanguage.googleapis.com/v1beta/interactions",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURLForUpstream(tt.upstream, tt.route, "gemini", tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURLForUpstream returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURLForUpstream = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayEndpointURLUsesCohereNativePaths(t *testing.T) {
	for _, tt := range []struct {
		name     string
		upstream domainaigateway.LLMUpstream
		endpoint string
		want     string
	}{
		{
			name: "rerank appends v2",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://api.cohere.com",
			},
			endpoint: "rerank",
			want:     "https://api.cohere.com/v2/rerank",
		},
		{
			name: "rerank keeps v2 suffix",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://api.cohere.com/v2",
			},
			endpoint: "rerank",
			want:     "https://api.cohere.com/v2/rerank",
		},
		{
			name: "models uses v1",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://api.cohere.com/v2",
			},
			endpoint: "models",
			want:     "https://api.cohere.com/v1/models",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURLForUpstream(tt.upstream, domainaigateway.LLMModelRoute{}, "cohere", tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURLForUpstream returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURLForUpstream = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayModelsFromResponse(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want []string
	}{
		{name: "OpenAI data", body: `{"data":[{"id":"gpt-5.6-sol"},{"id":"gpt-5.6-luna"}]}`, want: []string{"gpt-5.6-luna", "gpt-5.6-sol"}},
		{name: "Gemini models", body: `{"models":[{"name":"models/gemini-2.5-pro"}]}`, want: []string{"gemini-2.5-pro"}},
		{name: "invalid JSON", body: `{`, want: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := relayModelsFromResponse([]byte(tt.body)); !slices.Equal(got, tt.want) {
				t.Fatalf("relayModelsFromResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func relayEndpointURLForUpstream(upstream domainaigateway.LLMUpstream, route domainaigateway.LLMModelRoute, provider, endpoint string) (string, error) {
	return relayEndpointURLForTarget(relayport.Target{
		BaseURL: upstream.BaseURL, Model: route.UpstreamModel, Metadata: upstream.Metadata, RouteMetadata: route.Metadata,
	}, provider, endpoint)
}

var azureOpenAIPathCases = []struct {
	name     string
	upstream domainaigateway.LLMUpstream
	route    domainaigateway.LLMModelRoute
	endpoint string
	want     string
}{
	{
		name: "v1 chat appends openai v1",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
		},
		endpoint: "chat/completions",
		want:     "https://example.openai.azure.com/openai/v1/chat/completions",
	},
	{
		name: "v1 models keeps existing openai v1 suffix",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com/openai/v1",
		},
		endpoint: "models",
		want:     "https://example.openai.azure.com/openai/v1/models",
	},
	{
		name: "deployment chat uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-4o-prod",
		},
		endpoint: "chat/completions",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-4o-prod/chat/completions?api-version=2024-10-21",
	},
	{
		name: "route metadata overrides deployment",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com/openai",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
					"deployment": "upstream-deployment",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "route-model",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"deployment": "route-deployment",
				},
			},
		},
		endpoint: "embeddings",
		want:     "https://example.openai.azure.com/openai/deployments/route-deployment/embeddings?api-version=2024-10-21",
	},
	{
		name: "deployment images generations uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-image-prod",
		},
		endpoint: "images/generations",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-image-prod/images/generations?api-version=2024-10-21",
	},
	{
		name: "deployment images edits uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-image-prod",
		},
		endpoint: "images/edits",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-image-prod/images/edits?api-version=2024-10-21",
	},
	{
		name: "deployment images variations uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-image-prod",
		},
		endpoint: "images/variations",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-image-prod/images/variations?api-version=2024-10-21",
	},
	{
		name: "deployment audio speech uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "tts-prod",
		},
		endpoint: "audio/speech",
		want:     "https://example.openai.azure.com/openai/deployments/tts-prod/audio/speech?api-version=2024-10-21",
	},
	{
		name: "deployment audio transcriptions uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "whisper-prod",
		},
		endpoint: "audio/transcriptions",
		want:     "https://example.openai.azure.com/openai/deployments/whisper-prod/audio/transcriptions?api-version=2024-10-21",
	},
	{
		name: "deployment audio translations uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "whisper-prod",
		},
		endpoint: "audio/translations",
		want:     "https://example.openai.azure.com/openai/deployments/whisper-prod/audio/translations?api-version=2024-10-21",
	},
}

func TestApplyRelayUpstreamHeadersUsesBearerForFirstClassOpenAICompatibleProviders(t *testing.T) {
	for _, providerKind := range []string{"deepseek", "qwen", "openrouter"} {
		t.Run(providerKind, func(t *testing.T) {
			headers := http.Header{}
			applyRelayUpstreamHeaders(headers, relayport.Request{
				ProviderKind: providerKind,
				Endpoint:     "chat/completions",
				Headers:      http.Header{"Accept": []string{"application/json"}},
			}, map[string]any{
				"HTTP-Referer":  "https://soha.example",
				"Authorization": "Bearer should-not-forward",
			}, "sk-provider")

			if got := headers.Get("Authorization"); got != "Bearer sk-provider" {
				t.Fatalf("Authorization = %q", got)
			}
			if got := headers.Get("x-api-key"); got != "" {
				t.Fatalf("x-api-key should be empty, got %q", got)
			}
			if got := headers.Get("HTTP-Referer"); got != "https://soha.example" {
				t.Fatalf("HTTP-Referer = %q", got)
			}
		})
	}
}

func TestApplyRelayUpstreamHeadersUsesGoogleAPIKeyForGemini(t *testing.T) {
	headers := http.Header{}
	applyRelayUpstreamHeaders(headers, relayport.Request{
		ProviderKind: "gemini",
		Endpoint:     "generateContent",
		Headers:      http.Header{"Accept": []string{"application/json"}},
	}, map[string]any{
		"X-Goog-Api-Key":      "should-not-forward",
		"Authorization":       "Bearer should-not-forward",
		"X-Goog-User-Project": "soha-project",
	}, "gemini-provider-key")

	if got := headers.Get("x-goog-api-key"); got != "gemini-provider-key" {
		t.Fatalf("x-goog-api-key = %q", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should be empty, got %q", got)
	}
	if got := headers.Get("X-Goog-User-Project"); got != "soha-project" {
		t.Fatalf("X-Goog-User-Project = %q", got)
	}
}

func TestApplyRelayUpstreamHeadersFiltersCohereAPIKeyHeader(t *testing.T) {
	headers := http.Header{}
	applyRelayUpstreamHeaders(headers, relayport.Request{
		ProviderKind: "cohere",
		Endpoint:     "rerank",
		Headers: http.Header{
			"Accept":              []string{"application/json"},
			"OpenAI-Organization": []string{"org-should-not-forward"},
		},
	}, map[string]any{
		"Cohere-Api-Key": "should-not-forward",
		"X-Client-Name":  "opensoha",
	}, "cohere-provider-key")

	if got := headers.Get("Authorization"); got != "Bearer cohere-provider-key" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := headers.Get("Cohere-Api-Key"); got != "" {
		t.Fatalf("Cohere-Api-Key should be empty, got %q", got)
	}
	if got := headers.Get("OpenAI-Organization"); got != "" {
		t.Fatalf("OpenAI-Organization should be empty, got %q", got)
	}
	if got := headers.Get("X-Client-Name"); got != "opensoha" {
		t.Fatalf("X-Client-Name = %q", got)
	}
}

func TestApplyRelayUpstreamHeadersUsesAPIKeyForAzureOpenAI(t *testing.T) {
	headers := http.Header{}
	applyRelayUpstreamHeaders(headers, relayport.Request{
		ProviderKind: "azure-openai",
		Endpoint:     "chat/completions",
		Headers:      http.Header{"Accept": []string{"application/json"}},
	}, map[string]any{
		"api-key":       "should-not-forward",
		"Authorization": "Bearer should-not-forward",
		"X-Client-Name": "soha",
	}, "sk-azure")

	if got := headers.Get("api-key"); got != "sk-azure" {
		t.Fatalf("api-key = %q", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should be empty, got %q", got)
	}
	if got := headers.Get("X-Client-Name"); got != "soha" {
		t.Fatalf("X-Client-Name = %q", got)
	}
}
