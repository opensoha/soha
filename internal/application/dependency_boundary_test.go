package application

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionPackagesDoNotImportAdapters(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"github.com/opensoha/soha/internal/infrastructure/",
		"github.com/opensoha/soha/internal/repository/",
		"gorm.io/gorm",
	}
	technicalPrefixes := []string{
		"helm.sh/helm/",
		"k8s.io/api/",
		"k8s.io/apimachinery/",
		"k8s.io/client-go/",
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range forbidden {
				if strings.HasPrefix(importPath, prefix) {
					t.Errorf("%s imports adapter package %q", path, importPath)
				}
			}
			for _, prefix := range technicalPrefixes {
				if !strings.HasPrefix(importPath, prefix) {
					continue
				}
				normalizedPath := filepath.ToSlash(path)
				if !strings.HasPrefix(normalizedPath, "resource/") {
					continue
				}
				t.Errorf("%s imports Kubernetes/Helm technology package %q", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk application packages: %v", err)
	}
}
