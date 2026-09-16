package manifestruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestKustomizeOverlayKeepsSharedBaseGeneratorsAndImageDigests(t *testing.T) {
	files := kustomizeFixture()
	before, _ := json.Marshal(files)
	var previous string
	for _, environment := range []string{"dev", "prod"} {
		t.Run(environment, func(t *testing.T) {
			binding := domainmanifest.EnvironmentBinding{ID: environment, Namespace: environment, Kustomize: &domainmanifest.KustomizeOptions{
				EntryPath: "overlays/" + environment,
				Images: []domainmanifest.KustomizeImage{
					{Name: "api", NewName: "registry.example.com/team/api", Digest: "sha256:" + strings.Repeat("a", 64)},
					{Name: "worker", Digest: "sha256:" + strings.Repeat("b", 64)},
				},
			}}
			item := domainmanifest.Package{ID: "package", Renderer: domainmanifest.RendererKustomize}
			rendered, err := NewRenderer().Render(context.Background(), item, binding, files, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(rendered.Documents) != 2 || rendered.RenderedDigest == "" || rendered.RenderedDigest == previous {
				t.Fatalf("unexpected render: %#v", rendered)
			}
			previous = rendered.RenderedDigest
			for _, document := range rendered.Documents {
				if document.Namespace != environment || !strings.HasPrefix(document.Name, environment+"-") {
					t.Fatalf("wrong environment: %#v", document)
				}
				if document.Kind == "Deployment" && (!strings.Contains(document.Content, "registry.example.com/team/api@sha256:"+strings.Repeat("a", 64)) || !strings.Contains(document.Content, "worker@sha256:"+strings.Repeat("b", 64))) {
					t.Fatalf("images were not replaced: %s", document.Content)
				}
				if document.Kind == "ConfigMap" && !strings.Contains(document.Content, "hello=world") {
					t.Fatalf("generator input was lost: %s", document.Content)
				}
			}
			slices.Reverse(files)
			repeated, err := NewRenderer().Render(context.Background(), item, binding, files, 2)
			if err != nil || repeated.RenderedDigest != rendered.RenderedDigest {
				t.Fatalf("non-deterministic render: %s, %v", repeated.RenderedDigest, err)
			}
			slices.Reverse(files)
		})
	}
	after, _ := json.Marshal(files)
	if string(before) != string(after) {
		t.Fatal("render changed the stored input files")
	}
}

func TestKustomizeRejectsUnsafeOrUnresolvedInputs(t *testing.T) {
	for name, entry := range map[string]string{"absolute": "/tmp", "parent": "../base", "normalized-parent": "overlays/../../base", "remote-entry": "https://example.com/base", "unnormalized-entry": "./overlays/prod"} {
		t.Run(name, func(t *testing.T) {
			_, err := NewRenderer().Render(context.Background(), domainmanifest.Package{Renderer: domainmanifest.RendererKustomize}, domainmanifest.EnvironmentBinding{Kustomize: &domainmanifest.KustomizeOptions{EntryPath: entry}}, kustomizeFixture(), 0)
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("got %v, want invalid argument", err)
			}
		})
	}
	for name, configuration := range map[string]string{
		"remote":     "resources: [https://example.com/base]\n",
		"git-remote": "resources: [github.com/org/repo/deploy]\n",
		"escape":     "resources: [../outside.yaml]\n",
		"secret":     "secretGenerator:\n- name: secret\n  literals: [password=example]\n",
		"helm":       "helmCharts:\n- name: example\n  repo: https://example.com\n",
		"plugin":     "generators: [plugin.yaml]\n",
	} {
		t.Run(name, func(t *testing.T) {
			files := []domainmanifest.File{{Path: "Kustomization", Content: configuration}}
			_, err := NewRenderer().Render(context.Background(), domainmanifest.Package{Renderer: domainmanifest.RendererKustomize}, domainmanifest.EnvironmentBinding{}, files, 0)
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("got %v, want invalid argument", err)
			}
		})
	}
	t.Run("unmatched-image", func(t *testing.T) {
		binding := domainmanifest.EnvironmentBinding{Namespace: "dev", Kustomize: &domainmanifest.KustomizeOptions{EntryPath: "overlays/dev", Images: []domainmanifest.KustomizeImage{{Name: "missing", Digest: "sha256:" + strings.Repeat("a", 64)}}}}
		_, err := NewRenderer().Render(context.Background(), domainmanifest.Package{Renderer: domainmanifest.RendererKustomize}, binding, kustomizeFixture(), 0)
		if !errors.Is(err, apperrors.ErrInvalidArgument) || !strings.Contains(err.Error(), "did not produce") {
			t.Fatalf("unmatched mapping was accepted: %v", err)
		}
	})
}

func TestKustomizeGitSyncIncludesTextAndAllOverlayChanges(t *testing.T) {
	checkout := t.TempDir()
	for _, file := range kustomizeFixture() {
		name := filepath.Join(checkout, "deploy", file.Path)
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(file.Content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, _, err := readManifestRepositoryFiles(checkout, "deploy", nil, nil, domainmanifest.RendererKustomize)
	if err != nil || len(files) != len(kustomizeFixture()) {
		t.Fatalf("files = %d, error = %v", len(files), err)
	}
	for name, sourcePath := range map[string]string{"overlay-as-root": "deploy/overlays/prod", "excluded-base": "deploy"} {
		t.Run(name, func(t *testing.T) {
			entry := "."
			var excludes []string
			if name == "excluded-base" {
				entry = "overlays/prod"
				excludes = []string{"base/**"}
			}
			partial, _, err := readManifestRepositoryFiles(checkout, sourcePath, nil, excludes, domainmanifest.RendererKustomize)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := validateSyncedManifestFiles(partial, domainmanifest.RendererKustomize, entry); !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("missing shared base was accepted: %v", err)
			}
		})
	}
	digest, err := validateSyncedManifestFiles(files, domainmanifest.RendererKustomize, "overlays/dev", "overlays/prod")
	if err != nil {
		t.Fatal(err)
	}
	for index := range files {
		if files[index].Path == "overlays/prod/kustomization.yml" {
			files[index].Content += "commonAnnotations:\n  change: configuration-only\n"
		}
	}
	changed, err := validateSyncedManifestFiles(files, domainmanifest.RendererKustomize, "overlays/dev", "overlays/prod")
	if err != nil || digest == changed {
		t.Fatalf("overlay change lost: digest=%s error=%v", changed, err)
	}
	if err := os.Symlink(filepath.Join(checkout, "deploy"), filepath.Join(checkout, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readManifestRepositoryFiles(checkout, "linked", nil, nil, domainmanifest.RendererKustomize); err == nil {
		t.Fatal("symbolic source root was accepted")
	}
}

func kustomizeFixture() []domainmanifest.File {
	return []domainmanifest.File{
		{Path: "base/Kustomization", Content: "resources: [deployment.yaml]\nconfigMapGenerator:\n- name: settings\n  files: [settings.properties]\n"},
		{Path: "base/settings.properties", Content: "hello=world\n[not: valid: yaml\n"},
		{Path: "base/deployment.yaml", Content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  replicas: 1
  selector:
    matchLabels: {app: api}
  template:
    metadata:
      labels: {app: api}
    spec:
      containers:
      - name: api
        image: api:v1
      - name: worker
        image: worker:v1
`},
		{Path: "overlays/dev/kustomization.yaml", Content: "resources: [../../base]\nnamePrefix: dev-\n"},
		{Path: "overlays/prod/kustomization.yml", Content: "resources: [../../base]\nnamePrefix: prod-\nreplicas:\n- name: api\n  count: 3\n"},
	}
}
