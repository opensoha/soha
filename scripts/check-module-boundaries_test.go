package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, module := range []string{"contracts", "cli", "agent"} {
		if err := os.Mkdir(filepath.Join(root, module), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func write(t *testing.T, root, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentImports(t *testing.T) {
	for _, module := range []string{"contracts", "cli", "agent"} {
		for _, suffix := range []string{".go", "_test.go"} {
			t.Run(module+suffix, func(t *testing.T) {
				root := fixture(t)
				write(t, root, module+"/probe"+suffix, "package probe\nimport private \"github.com/opensoha/soha/internal/api\"\n")
				if err := check(root); err == nil || !strings.Contains(err.Error(), "must not import Core internal") {
					t.Fatalf("private import accepted: %v", err)
				}
			})
		}
	}
}

func TestPublicAndOwnedInternalImports(t *testing.T) {
	root := fixture(t)
	write(t, root, "cli/probe.go", "package probe\nimport (\"github.com/opensoha/soha/contracts/resource\"; \"github.com/opensoha/soha/cli/internal/profile\")\nconst note = \"github.com/opensoha/soha/internal/not_an_import\"\n")
	write(t, root, "agent/probe_test.go", "package probe\nimport _ \"github.com/opensoha/soha/agent/internal/runner\"\n")
	if err := check(root); err != nil {
		t.Fatal(err)
	}
}

func TestEscapedImportCannotBypassBoundary(t *testing.T) {
	root := fixture(t)
	write(t, root, "cli/probe.go", "package probe\nimport _ \"\\x67ithub.com/opensoha/soha/internal/secret\"\n")
	if err := check(root); err == nil {
		t.Fatal("escaped private import accepted")
	}
}

func TestInvalidImportsFailClosed(t *testing.T) {
	root := fixture(t)
	write(t, root, "contracts/probe.go", "package probe\nimport (not a string)\n")
	if err := check(root); err == nil {
		t.Fatal("unparseable imports accepted")
	}
}

func TestRedirectedSourceFailsClosed(t *testing.T) {
	root := fixture(t)
	outside := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(outside, []byte("package probe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "cli/probe.go")); err != nil {
		t.Fatal(err)
	}
	if err := check(root); err == nil {
		t.Fatal("source redirect accepted")
	}
}

func TestOrdinaryDirectoriesCannotHideGoImports(t *testing.T) {
	for _, directory := range []string{"dist", "coverage", "graphify-out", "node_modules"} {
		for _, suffix := range []string{".go", "_test.go"} {
			t.Run(directory+suffix, func(t *testing.T) {
				root := fixture(t)
				if err := os.Mkdir(filepath.Join(root, "cli", directory), 0700); err != nil {
					t.Fatal(err)
				}
				path := "cli/" + directory + "/probe" + suffix
				write(t, root, path, "package probe\nimport _ \"github.com/opensoha/soha/internal/api\"\n")
				if err := check(root); err == nil {
					t.Fatal("private import accepted in ordinary directory")
				}
				write(t, root, path, "package probe\nimport _ \"github.com/opensoha/soha/contracts/resource\"\n")
				if err := check(root); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestNonGoFileSymlinkDoesNotHideSource(t *testing.T) {
	root := fixture(t)
	tool := filepath.Join(t.TempDir(), "tool.js")
	if err := os.WriteFile(tool, []byte("// npm tool\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(tool, filepath.Join(root, "agent", "tool")); err != nil {
		t.Fatal(err)
	}
	if err := check(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "cli", "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := check(root); err == nil {
		t.Fatal("directory redirect accepted")
	}
}
