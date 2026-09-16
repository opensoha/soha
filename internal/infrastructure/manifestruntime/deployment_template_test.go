package manifestruntime

import (
	"context"
	"strings"
	"testing"

	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigyaml "sigs.k8s.io/yaml"
)

func TestDeploymentTemplateReplacesValuesWithoutYAMLInjection(t *testing.T) {
	source := domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "config.yaml", Content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: '${{ system.serviceKey }}'
  namespace: '${{ system.namespace }}'
data:
  message: '${{ parameters.message }}'
`}}}
	attack := "hello\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: injected"
	result, err := NewRenderer().RenderDeploymentTemplate(context.Background(), source, map[string]any{"message": attack}, map[string]string{"serviceKey": "api", "namespace": "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(result.Files[0].Content), &object); err != nil {
		t.Fatal(err)
	}
	data, ok := object["data"].(map[string]any)
	if !ok || data["message"] != attack || !strings.Contains(source.Files[0].Content, "${{") {
		t.Fatal("substitution changed the value or original template")
	}
	for name, content := range map[string]string{
		"unknown variable":   strings.ReplaceAll(source.Files[0].Content, "parameters.message", "parameters.missing"),
		"identity injection": strings.ReplaceAll(source.Files[0].Content, "system.namespace", "parameters.message"),
		"partial expression": strings.ReplaceAll(source.Files[0].Content, "${{ parameters.message }}", "prefix-${{ parameters.message }}"),
		"cluster scoped":     "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: other\n",
		"namespace escape":   strings.ReplaceAll(source.Files[0].Content, "${{ system.namespace }}", "production"),
	} {
		t.Run(name, func(t *testing.T) {
			input := domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "config.yaml", Content: content}}}
			if _, err := NewRenderer().RenderDeploymentTemplate(context.Background(), input, map[string]any{"message": attack}, map[string]string{"serviceKey": "api", "namespace": "test"}, nil); err == nil {
				t.Fatal("unsafe template was accepted")
			}
		})
	}
}

func TestDeploymentTemplatePreviewNameIsStableBoundedAndDistinct(t *testing.T) {
	source := domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "service.yaml", Content: "apiVersion: v1\nkind: Service\nmetadata:\n  name: ${{ system.previewServiceName }}\n  namespace: ${{ system.namespace }}\nspec:\n  ports: [{port: 80}]\n"}}}
	names := map[string]string{}
	for _, key := range []string{"api", strings.Repeat("a", 62) + "b", strings.Repeat("a", 62) + "c"} {
		for range 2 {
			result, err := NewRenderer().RenderDeploymentTemplate(t.Context(), source, nil, map[string]string{"serviceKey": key, "namespace": "test", "previewServiceName": "caller-override"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := sigyaml.Unmarshal([]byte(result.Files[0].Content), &object); err != nil {
				t.Fatal(err)
			}
			name := (&unstructured.Unstructured{Object: object}).GetName()
			if len(name) > 63 || name == key || name == "caller-override" || !strings.HasSuffix(name, "-preview") {
				t.Fatalf("invalid preview name: %s", name)
			}
			if old, ok := names[key]; ok && old != name {
				t.Fatal("preview identity changed")
			}
			for other, value := range names {
				if other != key && value == name {
					t.Fatal("different long service keys collided")
				}
			}
			names[key] = name
		}
	}
}

func TestBuiltinDeploymentTemplatesRenderTypedParameters(t *testing.T) {
	renderer := NewRenderer()
	for _, template := range domaincatalog.BuiltinDeploymentTemplates() {
		t.Run(template.Key, func(t *testing.T) {
			assertBuiltinDeploymentTemplate(t, renderer, template)
		})
	}
}

func assertBuiltinDeploymentTemplate(t *testing.T, renderer *Renderer, template domaincatalog.DeploymentTemplateSpec) {
	t.Helper()
	inputs := builtinTemplateInputs(template.Key)
	values, err := template.ParameterSchema.ResolveParameters(template.Defaults, inputs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := renderer.RenderDeploymentTemplate(context.Background(), template.Source, values, map[string]string{"serviceKey": "api", "namespace": "test"}, map[string]string{"main": "registry.invalid/api@sha256:" + strings.Repeat("1", 64)})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderer.Render(context.Background(), domainmanifest.Package{Renderer: source.Renderer}, domainmanifest.EnvironmentBinding{Namespace: "test"}, source.Files, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.Documents) == 0 {
		t.Fatal("no workload generated")
	}
	if template.Health.Mode == "job_complete" && rendered.Documents[0].Kind != "Job" {
		t.Fatal("Job template lost completion semantics")
	}
	if template.Key == "soha-gitops" {
		assertGitOpsTemplate(t, rendered.Documents)
	}
	if template.Key == "soha-http-periodic" {
		assertPeriodicTemplate(t, template, rendered.Documents)
	}
}

func builtinTemplateInputs(key string) map[string]any {
	if key == "soha-gitops" {
		return map[string]any{"repositoryId": "repo", "repositoryURL": "https://git.example/config.git", "commit": strings.Repeat("a", 40), "project": "test-project"}
	}
	if key == "soha-bluegreen" || key == "soha-canary" {
		inputs := map[string]any{"metricURL": "http://api-preview.test.svc:8080/metrics"}
		if key == "soha-canary" {
			inputs["routeMatch"] = "Host(\"app.example.com\")"
		}
		return inputs
	}
	return nil
}

func assertGitOpsTemplate(t *testing.T, documents []domainmanifest.RenderedDocument) {
	t.Helper()
	var application unstructured.Unstructured
	if err := sigyaml.Unmarshal([]byte(documents[0].Content), &application.Object); err != nil {
		t.Fatal(err)
	}
	if err := resourceruntime.ValidateArgoApplication(&application); err != nil {
		t.Fatal(err)
	}
	images, _, _ := unstructured.NestedStringSlice(application.Object, "spec", "source", "kustomize", "images")
	if len(documents) != 1 || len(images) != 1 || images[0] != "registry.invalid/api@sha256:"+strings.Repeat("1", 64) {
		t.Fatal("GitOps template lost its fixed artifact")
	}
}

func assertPeriodicTemplate(t *testing.T, template domaincatalog.DeploymentTemplateSpec, documents []domainmanifest.RenderedDocument) {
	t.Helper()
	if len(documents) != 3 || documents[0].Kind != "Deployment" || documents[1].Kind != "Service" || documents[2].Kind != "WorkloadCronJob" {
		t.Fatalf("periodic template lost its source workload: %#v", documents)
	}
	var object map[string]any
	if err := sigyaml.Unmarshal([]byte(documents[2].Content), &object); err != nil {
		t.Fatal(err)
	}
	suspended, _, _ := unstructured.NestedBool(object, "spec", "cronJobSpec", "suspend")
	sourceName, _, _ := unstructured.NestedString(object, "spec", "sourceRef", "name")
	if !suspended || sourceName != "api" || template.Health.Mode != "workload_ready" {
		t.Fatal("periodic task must start suspended and follow the same published service")
	}
}
