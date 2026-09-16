package sourceanalysis

import (
	"encoding/json"
	"encoding/xml"
	"path"
	"slices"
	"strings"
	"unicode"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/modfile"
)

const RuleVersion = "1"

// CandidateFiles is the bounded inventory; nested projects use the same files
// relative to the caller's explicitly selected project directory.
var CandidateFiles = []string{"Dockerfile", "go.mod", "go.work", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "pyproject.toml", "requirements.txt", "poetry.lock", "pom.xml", "build.gradle", "build.gradle.kts", "Cargo.toml", "settings.gradle", "settings.gradle.kts"}

// Identify only reads metadata. File contents never become commands or output.
func Identify(projectPath string, files map[string]string) sohaapi.RepositoryAnalysis {
	result := sohaapi.RepositoryAnalysis{ProjectPath: projectPath, RuleVersion: RuleVersion, Status: sohaapi.AnalysisUnrecognized,
		Candidates: []sohaapi.RepositoryAnalysisCandidate{}, EvidencePaths: []string{}, Warnings: []sohaapi.RepositoryAnalysisWarning{}}
	for _, name := range CandidateFiles {
		if _, ok := files[name]; ok {
			result.EvidencePaths = append(result.EvidencePaths, path.Join(projectPath, name))
		}
	}
	for _, language := range []string{"go", "node", "python", "java", "rust"} {
		candidate := identifyLanguage(language, files)
		if len(candidate.EvidencePaths) == 0 {
			continue
		}
		candidate.Language, candidate.ProjectPath = language, projectPath
		candidate.BuildMethods = []sohaapi.RepositoryAnalysisCandidateBuildMethods{sohaapi.AnalysisBuildpacks}
		if _, ok := files["Dockerfile"]; ok {
			candidate.BuildMethods = append([]sohaapi.RepositoryAnalysisCandidateBuildMethods{sohaapi.AnalysisDockerfile}, candidate.BuildMethods...)
			candidate.EvidencePaths = append(candidate.EvidencePaths, "Dockerfile")
		}
		for i, name := range candidate.EvidencePaths {
			candidate.EvidencePaths[i] = path.Join(projectPath, name)
		}
		result.Candidates = append(result.Candidates, candidate)
	}
	if len(result.Candidates) == 0 {
		if _, ok := files["Dockerfile"]; ok {
			result.Candidates = append(result.Candidates, sohaapi.RepositoryAnalysisCandidate{Language: "unknown", ProjectPath: projectPath,
				BuildMethods: []sohaapi.RepositoryAnalysisCandidateBuildMethods{sohaapi.AnalysisDockerfile}, EvidencePaths: []string{path.Join(projectPath, "Dockerfile")}})
		}
	}
	if len(result.Candidates) > 0 {
		result.Status = sohaapi.AnalysisIdentified
	}
	if len(result.Candidates) > 1 {
		result.Status = sohaapi.AnalysisMultipleCandidates
	}
	addWorkspaceWarning(&result, files)
	addWorkspaceCandidates(&result, files)
	addMetadataWarnings(&result, files)
	return result
}

func identifyLanguage(language string, files map[string]string) sohaapi.RepositoryAnalysisCandidate {
	var candidate sohaapi.RepositoryAnalysisCandidate
	switch language {
	case "go":
		candidate.EvidencePaths = present(files, "go.mod")
		candidate.PackageManager = "go"
		if parsed, err := modfile.ParseLax("go.mod", []byte(files["go.mod"]), nil); err == nil && parsed.Go != nil {
			candidate.VersionRange = parsed.Go.Version
		}
	case "node":
		candidate = identifyNode(files)
	case "python":
		candidate = identifyPython(files)
	case "java":
		candidate = identifyJava(files)
	case "rust":
		candidate.EvidencePaths, candidate.PackageManager = present(files, "Cargo.toml"), "cargo"
		var metadata struct {
			Package struct {
				RustVersion string `toml:"rust-version"`
			} `toml:"package"`
		}
		if toml.Unmarshal([]byte(files["Cargo.toml"]), &metadata) == nil {
			candidate.VersionRange = metadata.Package.RustVersion
		}
	}
	return candidate
}

func identifyNode(files map[string]string) sohaapi.RepositoryAnalysisCandidate {
	candidate := sohaapi.RepositoryAnalysisCandidate{EvidencePaths: present(files, "package.json"), PackageManager: "npm"}
	var metadata struct {
		PackageManager string            `json:"packageManager"`
		Engines        map[string]string `json:"engines"`
		Dependencies   map[string]any    `json:"dependencies"`
		Dev            map[string]any    `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(files["package.json"]), &metadata) == nil {
		candidate.VersionRange = metadata.Engines["node"]
		manager, _, _ := strings.Cut(metadata.PackageManager, "@")
		if slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, manager) {
			candidate.PackageManager = manager
		}
		for _, framework := range []string{"next", "vite", "express"} {
			if metadata.Dependencies[framework] != nil || metadata.Dev[framework] != nil {
				candidate.Framework = framework
				break
			}
		}
	}
	for _, lock := range []struct{ file, manager string }{{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"package-lock.json", "npm"}} {
		if _, ok := files[lock.file]; ok {
			if metadata.PackageManager == "" {
				candidate.PackageManager = lock.manager
			}
			candidate.EvidencePaths = append(candidate.EvidencePaths, lock.file)
		}
	}
	return candidate
}

func identifyPython(files map[string]string) sohaapi.RepositoryAnalysisCandidate {
	candidate := sohaapi.RepositoryAnalysisCandidate{EvidencePaths: present(files, "pyproject.toml", "requirements.txt"), PackageManager: "pip"}
	var metadata struct {
		Project struct {
			RequiresPython string `toml:"requires-python"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Dependencies map[string]any `toml:"dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if toml.Unmarshal([]byte(files["pyproject.toml"]), &metadata) == nil {
		candidate.VersionRange = metadata.Project.RequiresPython
		if metadata.Tool.Poetry.Dependencies != nil {
			candidate.PackageManager = "poetry"
			if candidate.VersionRange == "" {
				candidate.VersionRange, _ = metadata.Tool.Poetry.Dependencies["python"].(string)
			}
		}
	}
	if _, ok := files["poetry.lock"]; ok && len(candidate.EvidencePaths) > 0 {
		candidate.PackageManager = "poetry"
		candidate.EvidencePaths = append(candidate.EvidencePaths, "poetry.lock")
	}
	return candidate
}

func identifyJava(files map[string]string) sohaapi.RepositoryAnalysisCandidate {
	candidate := sohaapi.RepositoryAnalysisCandidate{EvidencePaths: present(files, "pom.xml", "build.gradle", "build.gradle.kts"), PackageManager: "gradle"}
	if _, ok := files["pom.xml"]; !ok {
		return candidate
	}
	candidate.PackageManager = "maven"
	var metadata struct {
		Parent struct {
			GroupID string `xml:"groupId"`
		} `xml:"parent"`
		Properties struct {
			Release string `xml:"maven.compiler.release"`
			Source  string `xml:"maven.compiler.source"`
			Java    string `xml:"java.version"`
		} `xml:"properties"`
		Dependencies []struct {
			GroupID string `xml:"groupId"`
		} `xml:"dependencies>dependency"`
	}
	if xml.Unmarshal([]byte(files["pom.xml"]), &metadata) == nil {
		for _, version := range []string{metadata.Properties.Release, metadata.Properties.Source, metadata.Properties.Java} {
			if version != "" {
				candidate.VersionRange = version
				break
			}
		}
		if metadata.Parent.GroupID == "org.springframework.boot" || slices.ContainsFunc(metadata.Dependencies, func(dep struct {
			GroupID string `xml:"groupId"`
		}) bool {
			return dep.GroupID == "org.springframework.boot"
		}) {
			candidate.Framework = "spring-boot"
		}
	}
	return candidate
}

func present(files map[string]string, names ...string) []string {
	var result []string
	for _, name := range names {
		if _, ok := files[name]; ok {
			result = append(result, name)
		}
	}
	return result
}

func addWorkspaceWarning(result *sohaapi.RepositoryAnalysis, files map[string]string) {
	workspaceFiles := present(files, "go.work", "settings.gradle", "settings.gradle.kts")
	var node struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal([]byte(files["package.json"]), &node) == nil && len(node.Workspaces) > 0 {
		workspaceFiles = append(workspaceFiles, "package.json")
	}
	var rust struct {
		Workspace map[string]any `toml:"workspace"`
	}
	if toml.Unmarshal([]byte(files["Cargo.toml"]), &rust) == nil && rust.Workspace != nil {
		workspaceFiles = append(workspaceFiles, "Cargo.toml")
	}
	for _, file := range workspaceFiles {
		result.Warnings = append(result.Warnings, sohaapi.RepositoryAnalysisWarning{Code: "select_project_directory", Message: "Workspace metadata found; select the deployable project's directory before choosing a build method.", Path: path.Join(result.ProjectPath, file)})
	}
}

func addWorkspaceCandidates(result *sohaapi.RepositoryAnalysis, files map[string]string) {

	if work, err := modfile.ParseWork("go.work", []byte(files["go.work"]), nil); err == nil && work != nil {
		for _, use := range work.Use {
			addWorkspaceCandidate(result, use.Path, "go", "go", "go.work")
		}
	}
	var maven struct {
		Modules []string `xml:"modules>module"`
	}
	if xml.Unmarshal([]byte(files["pom.xml"]), &maven) == nil {
		for _, module := range maven.Modules {
			addWorkspaceCandidate(result, module, "java", "maven", "pom.xml")
		}
	}
	var rust struct {
		Workspace struct {
			Members []string `toml:"members"`
		} `toml:"workspace"`
	}
	if toml.Unmarshal([]byte(files["Cargo.toml"]), &rust) == nil {
		for _, member := range rust.Workspace.Members {
			addWorkspaceCandidate(result, member, "rust", "cargo", "Cargo.toml")
		}
	}
	var node struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal([]byte(files["package.json"]), &node) == nil {
		var directories []string
		if json.Unmarshal(node.Workspaces, &directories) != nil {
			var workspace struct {
				Packages []string `json:"packages"`
			}
			if json.Unmarshal(node.Workspaces, &workspace) == nil {
				directories = workspace.Packages
			}
		}
		for _, directory := range directories {
			addWorkspaceCandidate(result, directory, "node", "", "package.json")
		}
	}
	if len(result.Candidates) == 1 {
		result.Status = sohaapi.AnalysisIdentified
	}
	if len(result.Candidates) > 1 {
		result.Status = sohaapi.AnalysisMultipleCandidates
	}
}

func addWorkspaceCandidate(result *sohaapi.RepositoryAnalysis, directory, language, manager, evidence string) {
	if directory == "" || directory == "." || path.IsAbs(directory) || strings.ContainsAny(directory, "*?[]\\:") || strings.ContainsFunc(directory, unicode.IsControl) || slices.Contains(strings.Split(directory, "/"), "..") {
		return
	}
	project := path.Join(result.ProjectPath, directory)
	if slices.ContainsFunc(result.Candidates, func(candidate sohaapi.RepositoryAnalysisCandidate) bool {
		return candidate.ProjectPath == project && candidate.Language == language
	}) {
		return
	}
	result.Candidates = append(result.Candidates, sohaapi.RepositoryAnalysisCandidate{Language: language, PackageManager: manager, ProjectPath: project,
		BuildMethods: []sohaapi.RepositoryAnalysisCandidateBuildMethods{sohaapi.AnalysisBuildpacks}, EvidencePaths: []string{path.Join(result.ProjectPath, evidence)}})
}

func addMetadataWarnings(result *sohaapi.RepositoryAnalysis, files map[string]string) {
	for _, name := range []string{"go.mod", "go.work", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml"} {
		content, ok := files[name]
		if !ok {
			continue
		}
		var err error
		var value map[string]any
		switch name {
		case "go.mod":
			_, err = modfile.ParseLax(name, []byte(content), nil)
		case "go.work":
			_, err = modfile.ParseWork(name, []byte(content), nil)
		case "package.json":
			err = json.Unmarshal([]byte(content), &value)
		case "pyproject.toml", "Cargo.toml":
			err = toml.Unmarshal([]byte(content), &value)
		case "pom.xml":
			err = xml.Unmarshal([]byte(content), new(struct{}))
		}
		if err != nil {
			result.Warnings = append(result.Warnings, sohaapi.RepositoryAnalysisWarning{Code: "invalid_metadata", Message: "Metadata could not be parsed; inspect this file before choosing a build method.", Path: path.Join(result.ProjectPath, name)})
		}
	}
}
