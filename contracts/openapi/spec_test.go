package openapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEmbeddedSpecsMatchMetadata(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
		want string
	}{
		{name: "yaml", body: YAML(), want: YAMLSHA256},
		{name: "json", body: JSON(), want: JSONSHA256},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := fmt.Sprintf("%x", sha256.Sum256(test.body))
			if got != test.want {
				t.Fatalf("sha256 = %s, want %s", got, test.want)
			}
		})
	}

	var document struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(JSON(), &document); err != nil {
		t.Fatalf("decode embedded JSON: %v", err)
	}
	if document.Info.Version != Version {
		t.Fatalf("info.version = %q, want %q", document.Info.Version, Version)
	}
}

func TestTopologySuccessResponsePreservesDescription(t *testing.T) {
	var document struct {
		Paths map[string]struct {
			Get struct {
				Responses map[string]map[string]json.RawMessage `json:"responses"`
			} `json:"get"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(JSON(), &document); err != nil {
		t.Fatal(err)
	}
	response := document.Paths["/clusters/{clusterID}/network/topology"].Get.Responses["200"]
	if len(response) == 0 {
		t.Fatal("missing topology success response")
	}
	for key := range response {
		switch key {
		case "description", "headers", "content", "links":
		default:
			if !strings.HasPrefix(key, "x-") {
				t.Errorf("unexpected topology response field %q", key)
			}
		}
	}
	var description string
	if err := json.Unmarshal(response["description"], &description); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(description, "Kubernetes, Gateway API, Traefik and APISIX topology,") {
		t.Fatalf("topology response description was truncated: %q", description)
	}
}

func TestPodWebSocketMetadataPreservesHTTPRouteSurface(t *testing.T) {
	var document struct {
		Paths   map[string]json.RawMessage `json:"paths"`
		Streams map[string]struct {
			Get struct {
				Permissions struct {
					Keys []string `json:"keys"`
				} `json:"x-soha-permissions"`
				Parameters []struct {
					Name   string `json:"name"`
					Schema struct {
						Type      string `json:"type"`
						MinLength int    `json:"minLength"`
					} `json:"schema"`
				} `json:"parameters"`
			} `json:"get"`
		} `json:"x-soha-websocket-paths"`
	}
	if err := json.Unmarshal(JSON(), &document); err != nil {
		t.Fatal(err)
	}
	for suffix, permission := range map[string]string{"logs/stream": "platform.pods.logs", "terminal": "platform.pods.exec"} {
		path := "/clusters/{clusterID}/workloads/pods/{podName}/" + suffix
		if _, exists := document.Paths[path]; exists {
			t.Fatalf("WebSocket target entered the HTTP client route surface: %s", path)
		}
		stream, exists := document.Streams[path]
		if !exists || len(stream.Get.Permissions.Keys) != 1 || stream.Get.Permissions.Keys[0] != permission {
			t.Fatalf("missing WebSocket target permission: %s", path)
		}
		uidQuery := false
		for _, parameter := range stream.Get.Parameters {
			if parameter.Name == "expectedUid" && parameter.Schema.Type == "string" && parameter.Schema.MinLength == 1 {
				uidQuery = true
			}
		}
		if !uidQuery {
			t.Fatalf("missing non-empty expectedUid query metadata: %s", path)
		}
	}
}
