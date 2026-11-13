package domain

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDomainPackagesDoNotImportImplementations(t *testing.T) {
	t.Parallel()

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !domainImportAllowed(importPath) {
				t.Errorf("%s imports non-domain dependency %q", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk domain packages: %v", err)
	}
}

func domainImportAllowed(path string) bool {
	for _, technology := range []string{"net/http", "crypto/tls", "database/sql", "C"} {
		if path == technology || strings.HasPrefix(path, technology+"/") {
			return false
		}
	}
	root, _, _ := strings.Cut(path, "/")
	if !strings.Contains(root, ".") {
		return true
	}
	// Shared schemas and value helpers are used by domain types and validation.
	return strings.HasPrefix(path, "github.com/opensoha/soha/internal/domain/") ||
		strings.HasPrefix(path, "github.com/opensoha/soha/contracts/") ||
		path == "github.com/opensoha/soha/internal/networkprotocol" ||
		path == "github.com/opensoha/soha/internal/platform/apperrors" ||
		path == "go.yaml.in/yaml/v3" ||
		path == "github.com/google/uuid" || path == "github.com/santhosh-tekuri/jsonschema/v6"
}

func TestDomainImportBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"net/netip", true},
		{"github.com/opensoha/soha/contracts/gen/go/sohaapi", true},
		{"go.yaml.in/yaml/v3", true},
		{"go.yaml.in/yaml/v3/decoder", false},
		{"gopkg.in/yaml.v3", false},
		{"sigs.k8s.io/yaml", false},
		{"net/http/httputil", false},
		{"crypto/tls", false},
		{"database/sql", false},
		{"gorm.io/gorm", false},
		{"k8s.io/apimachinery/pkg/util/validation", false},
		{"helm.sh/helm/v4/pkg/action", false},
		{"github.com/aws/aws-sdk-go-v2/service/ec2", false},
		{"github.com/opensoha/soha/internal/application/virtualization/consoleport", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got := domainImportAllowed(tc.path); got != tc.want {
				t.Fatalf("domainImportAllowed(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
