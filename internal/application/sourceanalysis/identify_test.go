package sourceanalysis

import (
	"slices"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestIdentifyMetadataWithoutInventingWebFrameworks(t *testing.T) {
	for _, tc := range []struct {
		name, language, manager, version, framework string
		files                                       map[string]string
	}{
		{"go command", "go", "go", "1.26", "", map[string]string{"go.mod": "module example.com/tool\n\ngo 1.26\n"}},
		{"node", "node", "pnpm", ">=22", "next", map[string]string{"package.json": `{"engines":{"node":">=22"},"packageManager":"pnpm@10.0.0","dependencies":{"next":"16"}}`, "pnpm-lock.yaml": ""}},
		{"python library", "python", "pip", ">=3.12", "", map[string]string{"pyproject.toml": "[project]\nrequires-python = '>=3.12'"}},
		{"maven library", "java", "maven", "21", "", map[string]string{"pom.xml": `<project><properties><maven.compiler.release>21</maven.compiler.release></properties></project>`}},
		{"spring", "java", "maven", "", "spring-boot", map[string]string{"pom.xml": `<project><parent><groupId>org.springframework.boot</groupId></parent></project>`}},
		{"rust", "rust", "cargo", "1.85", "", map[string]string{"Cargo.toml": "[package]\nrust-version = '1.85'"}},
		{"gradle", "java", "gradle", "", "", map[string]string{"build.gradle.kts": `plugins { java }`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := Identify("services/api", tc.files)
			if result.Status != sohaapi.AnalysisIdentified || len(result.Candidates) != 1 {
				t.Fatalf("identification: %+v", result)
			}
			got := result.Candidates[0]
			if got.Language != tc.language || got.PackageManager != tc.manager || got.VersionRange != tc.version || got.Framework != tc.framework || got.ProjectPath != "services/api" {
				t.Fatalf("candidate: %+v", got)
			}
			if got.BuildMethods[0] != sohaapi.AnalysisBuildpacks || !slices.Contains(result.EvidencePaths, got.EvidencePaths[0]) {
				t.Fatalf("missing evidence: %+v", result)
			}
		})
	}
	result := Identify(".", map[string]string{"go.mod": "module example.com/api\ngo 1.26\n", "package.json": `{"workspaces":["services/*"]}`, "Dockerfile": "RUN should-never-execute"})
	if result.Status != sohaapi.AnalysisMultipleCandidates || len(result.Candidates) != 2 || len(result.Warnings) != 1 || result.Candidates[0].BuildMethods[0] != sohaapi.AnalysisDockerfile {
		t.Fatalf("polyglot workspace or Dockerfile priority lost: %+v", result)
	}
	if result := Identify(".", map[string]string{}); result.Status != sohaapi.AnalysisUnrecognized || result.Candidates == nil || result.Warnings == nil {
		t.Fatalf("empty repository: %+v", result)
	}
}

func TestWorkspaceCandidatesAndMalformedMetadata(t *testing.T) {
	result := Identify(".", map[string]string{"go.work": "go 1.26.0\nuse (\n ./services/api\n ./workers/jobs\n)\n"})
	if result.Status != sohaapi.AnalysisMultipleCandidates || len(result.Candidates) != 2 || result.Candidates[0].ProjectPath != "services/api" {
		t.Fatalf("workspace: %+v", result)
	}
	result = Identify(".", map[string]string{"pom.xml": "<project><modules><module>api</module><module>worker</module></modules></project>"})
	if result.Status != sohaapi.AnalysisMultipleCandidates || len(result.Candidates) != 3 {
		t.Fatalf("maven modules: %+v", result)
	}
	result = Identify(".", map[string]string{"package.json": "{broken"})
	if len(result.Warnings) == 0 || result.Warnings[0].Code != "invalid_metadata" {
		t.Fatalf("malformed metadata: %+v", result)
	}
}
