package resource

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHighRiskApplicationPackagesDoNotImportConcreteAdapters(t *testing.T) {
	t.Parallel()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	applicationDir := filepath.Dir(filepath.Dir(filename))
	for _, packageName := range []string{"resource", "workflow", "aigateway"} {
		packageDir := filepath.Join(applicationDir, packageName)
		entries, err := os.ReadDir(packageDir)
		if err != nil {
			t.Fatalf("read %s: %v", packageName, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(packageDir, entry.Name())
			// packageDir is derived from this test file and entry comes from os.ReadDir(packageDir).
			//nolint:gosec // the test only reads Go files enumerated from the fixed application package directory
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for _, forbidden := range []string{
				"github.com/opensoha/soha/internal/infrastructure/",
				"github.com/opensoha/soha/internal/repository/",
			} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("%s imports forbidden concrete adapter path %q", path, forbidden)
				}
			}
		}
	}
}
