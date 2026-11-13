package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	deliverydoc "github.com/opensoha/soha-contracts/delivery"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const deliveryDocumentFixture = "apiVersion: delivery.soha.io/v1alpha1\nkind: BuildTemplate\nmetadata:\n  name: cli-build\nspec:\n  builderKind: custom\n  dockerfileTemplate: FROM scratch\n"

func TestDeliveryDocumentOfflineValidation(t *testing.T) {
	for _, tc := range []struct {
		name, content, diagnostic string
	}{
		{"valid", deliveryDocumentFixture, ""},
		{"duplicate", strings.Replace(deliveryDocumentFixture, "  name: cli-build", "  name: cli-build\n  name: other", 1), "duplicate_key"},
		{"unknown", strings.Replace(deliveryDocumentFixture, "spec:", "extra: secret-value\nspec:", 1), "schema_additionalProperties"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errs bytes.Buffer
			code := Run(context.Background(), []string{"delivery", "documents", "validate", "--file", "-"}, Runtime{In: strings.NewReader(tc.content), Out: &out, Err: &errs, ConfigPath: filepath.Join(t.TempDir(), "missing.json")})
			var result struct {
				Valid       bool                     `json:"valid"`
				Diagnostics []deliverydoc.Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("invalid output: %s %s", out.String(), errs.String())
			}
			if (code == 0) != (tc.diagnostic == "") || result.Valid != (code == 0) {
				t.Fatalf("exit=%d valid=%v: %s", code, result.Valid, errs.String())
			}
			if tc.diagnostic != "" && (len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != tc.diagnostic) {
				t.Fatalf("diagnostics=%+v", result.Diagnostics)
			}
			if strings.Contains(out.String()+errs.String(), "secret-value") {
				t.Fatal("invalid document leaked a raw value")
			}
		})
	}
}

func TestDeliveryDocumentsPreviewAndRetryImport(t *testing.T) {
	requests, imports := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") == "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/delivery/documents/preview":
			var input sohaapi.DeliveryDocumentPreviewInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Files) != 1 || input.Files[0].TargetID != "template-1" || input.Files[0].ExpectedRevision != 3 || input.ValidateOnly {
				t.Errorf("incorrect preview input: %+v %v", input, err)
			}
			_, _ = w.Write([]byte(`{"data":{"valid":true,"id":"preview-1","candidateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","candidates":[],"diagnostics":[]}}`))
		case "/api/v1/delivery/documents/imports/preview-1/apply":
			imports++
			var input sohaapi.DeliveryDocumentApplyInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.IdempotencyKey != "attempt-1" || input.CandidateDigest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
				t.Errorf("incorrect import input: %+v %v", input, err)
			}
			if imports == 1 {
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte(`{"error":{"code":"timeout","message":"uncertain"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"previewId":"preview-1","objects":[{"id":"template-1","kind":"BuildTemplate","path":"build.yaml","revision":4,"action":"update"}]}}`))
		default:
			t.Errorf("unexpected endpoint; import must not execute or publish: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config := writeTestConfig(t, server.URL)
	raw, _ := json.Marshal(sohaapi.DeliveryDocumentPreviewInput{Files: []sohaapi.DeliveryDocumentFile{{Path: "build.yaml", Content: deliveryDocumentFixture, TargetID: "template-1", ExpectedRevision: 3}}})
	for i, args := range [][]string{
		{"preview", "--input", "-"},
		{"import", "--preview-id", "preview-1", "--candidate-digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--idempotency-key", "attempt-1", "--yes"},
		{"import", "--preview-id", "preview-1", "--candidate-digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--idempotency-key", "attempt-1", "--yes"},
	} {
		var out, errs bytes.Buffer
		args = append(append([]string{"delivery", "documents"}, args...), "--profile", "dev")
		code := Run(context.Background(), args, Runtime{In: bytes.NewReader(raw), Out: &out, Err: &errs, ConfigPath: config})
		if (code == 0) != (i != 1) || requests != i+1 {
			t.Fatalf("step=%d exit=%d requests=%d: %s", i, code, requests, errs.String())
		}
		if i == 1 && !strings.Contains(errs.String(), "same preview ID") {
			t.Fatalf("missing retry instructions: %s", errs.String())
		}
	}
}

func TestDeliveryDocumentsRejectInputBeforeNetworking(t *testing.T) {
	for _, tc := range []struct {
		args []string
		in   string
	}{
		{[]string{"preview", "--file", "-"}, "\xff"},
		{[]string{"preview", "--input", "-"}, "{\"files\":[{\"path\":\"a.yaml\",\"content\":\"\xff\"}]}"},
		{[]string{"preview", "--file", "-"}, strings.Repeat("a", deliverydoc.MaxFileBytes+1)},
		{[]string{"preview", "--input", "-"}, `{"files":[{"path":"../a.yaml","content":"x"}]}`},
		{[]string{"preview", "--input", "-"}, `{"files":[{"path":"a.yaml","content":"x","targetId":"t"}]}`},
		{[]string{"preview", "--input", "-"}, `{"files":[{"path":"a","content":"x"},{"path":"a","content":"x"}]}`},
		{[]string{"import", "--preview-id", "p", "--candidate-digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--idempotency-key", "attempt-1"}, "n\n"},
		{[]string{"import", "--preview-id", "../p", "--candidate-digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--idempotency-key", "attempt-1", "--yes"}, ""},
		{[]string{"import", "--preview-id", "p", "--candidate-digest", "d", "--idempotency-key", "short", "--yes"}, ""},
		{[]string{"import", "--preview-id", "p", "--candidate-digest", "d", "--yes"}, ""},
		{[]string{"export", "Workflow", "id", "--version", "2"}, ""},
		{[]string{"export", "BuildTemplate", "id"}, ""},
	} {
		var out, errs bytes.Buffer
		code := Run(context.Background(), append([]string{"delivery", "documents"}, tc.args...), Runtime{In: strings.NewReader(tc.in), Out: &out, Err: &errs, ConfigPath: filepath.Join(t.TempDir(), "missing.json")})
		if code == 0 || strings.Contains(errs.String(), "profile") || out.Len() != 0 {
			t.Fatalf("unexpected result: %v exit=%d: %s %s", tc.args, code, out.String(), errs.String())
		}
	}
}

func TestDeliveryDocumentExportIsReimportableAndDoesNotOverwrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v1/delivery/documents/BuildTemplate/template-1/export?format=yaml&version=2" || r.Header.Get("Authorization") == "" {
			t.Errorf("unexpected export: %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"content": deliveryDocumentFixture, "format": "yaml"}})
	}))
	defer server.Close()
	config, destination := writeTestConfig(t, server.URL), filepath.Join(t.TempDir(), "export.yaml")
	for i := range 3 {
		var out, errs bytes.Buffer
		args := []string{"delivery", "documents", "export", "BuildTemplate", "template-1", "--version", "2", "--profile", "dev"}
		if i > 0 {
			args = append(args, "--out", destination)
		}
		code := Run(context.Background(), args, Runtime{Out: &out, Err: &errs, ConfigPath: config})
		if (code == 0) != (i < 2) {
			t.Fatalf("step=%d exit=%d: %s", i, code, errs.String())
		}
		if i == 0 {
			if _, diagnostics := deliverydoc.Parse("export.yaml", out.Bytes()); len(diagnostics) > 0 {
				t.Fatalf("export contains an envelope or changed document: %+v", diagnostics)
			}
		}
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != deliveryDocumentFixture {
		t.Fatalf("export changed: %v", err)
	}
}
