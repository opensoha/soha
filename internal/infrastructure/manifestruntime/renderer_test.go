package manifestruntime

import (
	"context"
	"strings"
	"testing"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

func TestRendererIsDeterministicAndDefaultsNamespace(t *testing.T) {
	renderer := NewRenderer()
	item := domainmanifest.Package{ID: "package-1", Renderer: domainmanifest.RendererRaw}
	binding := domainmanifest.EnvironmentBinding{ID: "binding-1", Namespace: "payments", Overlay: map[string]string{"IMAGE": "registry.example/app:v1"}}
	files := []domainmanifest.File{
		{Path: "service.yaml", Content: "apiVersion: v1\nkind: Service\nmetadata:\n  name: api\nspec:\n  selector:\n    app: api\n"},
		{Path: "deployment.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          image: ${IMAGE}\n"},
	}

	first, err := renderer.Render(context.Background(), item, binding, files, 3)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	second, err := renderer.Render(context.Background(), item, binding, []domainmanifest.File{files[1], files[0]}, 3)
	if err != nil {
		t.Fatalf("Render() reordered error = %v", err)
	}
	if first.RenderedDigest != second.RenderedDigest {
		t.Fatalf("render digest changed with input order: %s != %s", first.RenderedDigest, second.RenderedDigest)
	}
	if len(first.Documents) != 2 || first.Documents[0].Namespace != "payments" || first.Documents[1].Namespace != "payments" {
		t.Fatalf("documents = %#v, want namespace defaulted", first.Documents)
	}
	if !strings.Contains(first.Documents[0].Content+first.Documents[1].Content, "registry.example/app:v1") {
		t.Fatal("rendered documents did not apply the binding overlay")
	}
}

func TestRendererRejectsInlineSecretAndEscapingPath(t *testing.T) {
	renderer := NewRenderer()
	item := domainmanifest.Package{ID: "package-1", Renderer: domainmanifest.RendererRaw}
	binding := domainmanifest.EnvironmentBinding{ID: "binding-1", Namespace: "default"}

	_, err := renderer.Render(context.Background(), item, binding, []domainmanifest.File{{Path: "secret.yaml", Content: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: token\nstringData:\n  token: cleartext\n"}}, 0)
	if err == nil || !strings.Contains(err.Error(), "inline secret data") {
		t.Fatalf("Render() error = %v, want inline secret rejection", err)
	}
	_, err = renderer.Render(context.Background(), item, binding, []domainmanifest.File{{Path: "../escape.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: escaped\n"}}, 0)
	if err == nil || !strings.Contains(err.Error(), "escapes package root") {
		t.Fatalf("Render() error = %v, want escaping path rejection", err)
	}
}
