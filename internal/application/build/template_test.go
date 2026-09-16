package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
)

func TestBuildTemplateVariablesAndDockerfileReachExecution(t *testing.T) {
	source := &domainapp.BuildSource{Type: domainapp.BuildSourceTypePlatformTemplate, Config: map[string]any{
		"variables": map[string]any{"version": "1.37.0", "enabled": false},
	}}
	metadata := map[string]any{
		"buildTemplateVariableSchema": map[string]any{
			"version": map[string]any{"type": "string", "required": true},
			"enabled": map[string]any{"type": "boolean"},
			"count":   map[string]any{"type": "integer"},
		},
		"buildTemplateDefaultVariables":   map[string]any{"version": "old", "enabled": true, "count": 3},
		"variables":                       map[string]any{"count": 0},
		"buildTemplateDockerfileTemplate": "FROM busybox:{{version}}\nLABEL count={{count}} enabled={{enabled}}",
	}
	before, _ := json.Marshal(source)
	commands, err := buildExecutionCommands(source, metadata, "registry.example/api:test")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(commands[1], "executor --dockerfile='.soha-template.Dockerfile'") {
		t.Fatalf("not a real container build: %v", commands)
	}
	if metadata["renderedDockerfile"] != "FROM busybox:1.37.0\nLABEL count=0 enabled=false" {
		t.Fatalf("unresolved defaults: %v", metadata["renderedDockerfile"])
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("render mutated editable source configuration")
	}
	// Re-preparing for a frozen batch must retain the same inputs after JSON storage.
	encoded, _ := json.Marshal(metadata)
	var restored map[string]any
	_ = json.Unmarshal(encoded, &restored)
	again, err := buildExecutionCommands(source, restored, "registry.example/api:test")
	if err != nil || strings.Join(again, "\n") != strings.Join(commands, "\n") {
		t.Fatalf("snapshot drift: %v", err)
	}
	dir := t.TempDir()
	// #nosec G204 -- Execute locally rendered fixtures to prove shell escaping and file creation.
	command := exec.Command("sh", "-c", commands[0])
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("materialize Dockerfile: %s %v", output, err)
	}
	// #nosec G304 -- Fixed output filename under this test's private temporary directory.
	content, err := os.ReadFile(filepath.Join(dir, ".soha-template.Dockerfile"))
	if err != nil || string(content) != fmt.Sprint(metadata["renderedDockerfile"])+"\n" {
		t.Fatalf("wrong file: %s %v", content, err)
	}
	// #nosec G204 -- Re-execute the same fixture to verify overwrite rejection.
	command = exec.Command("sh", "-c", commands[0])
	command.Dir = dir
	if command.Run() == nil {
		t.Fatal("overwrote an existing checkout file")
	}
}

func TestBuildTemplateRejectsInvalidValuesAndKeepsFreeTextInData(t *testing.T) {
	source := &domainapp.BuildSource{Type: domainapp.BuildSourceTypePlatformTemplate}
	for _, values := range []map[string]any{nil, {"version": 2}, {"version": "1", "unknown": true}, {"version": "1\nRUN touch injected"}, {"version": "$(touch injected)"}, {"version": "1", "IMAGE_REF": "bad"}} {
		_, err := buildExecutionCommands(source, map[string]any{
			"buildTemplateVariableSchema": map[string]any{"version": map[string]any{"type": "string", "required": true}},
			"variables":                   values, "buildTemplateCommands": []string{"echo {{version}}"},
		}, "image:test")
		if err == nil {
			t.Fatal("invalid variable reached execution")
		}
	}
	value := "hello ' \" $(touch injected); `touch injected`\nsecond line"
	commands, err := buildExecutionCommands(source, map[string]any{
		"buildTemplateVariableSchema": map[string]any{"message": map[string]any{"type": "string"}},
		"variables":                   map[string]any{"message": value},
		"buildTemplateCommands":       []any{`printf '%s' "$SOHA_BUILD_message" > captured`},
	}, "image:test")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// #nosec G204 -- Execute locally rendered fixtures to prove shell escaping and file creation.
	command := exec.Command("sh", "-c", strings.Join(commands, "\n"))
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("execute: %s %v", output, err)
	}
	// #nosec G304 -- Fixed output filename under this test's private temporary directory.
	actual, _ := os.ReadFile(filepath.Join(dir, "captured"))
	if string(actual) != value {
		t.Fatal("free text changed during execution")
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("variable was executed as shell code")
	}
}
