package application

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestProductionPackagesDoNotImportAdapters(t *testing.T) {
	t.Parallel()

	// Freeze existing exceptions at the file and dependency, so they cannot spread.
	legacy := map[string]map[string]string{
		"delivery/batch_access.go": {
			"k8s.io/api/core/v1":                "Delivery decodes Service access candidates",
			"k8s.io/api/networking/v1":          "Delivery decodes Ingress access candidates",
			"k8s.io/apimachinery/pkg/util/yaml": "Delivery reads multi-document workload inputs",
		},
		"manifest/delivery_gitops.go": {
			"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured": "Manifest reads GitOps document projections",
		},
		"manifest/rollout.go": {
			"k8s.io/apimachinery/pkg/api/errors": "Manifest recognizes rollout conflicts",
		},
		"virtualization/worker.go": {
			"k8s.io/apimachinery/pkg/api/resource": "Virtualization converts KubeVirt CPU and memory quantities",
		},
		"aigateway/connector_runtime_provider.go": {
			"net/http.Client":                "Gateway connector manifest and action client",
			"net/http.DefaultClient":         "Gateway connector client fallback",
			"net/http.NewRequestWithContext": "Gateway connector manifest and action requests",
		},
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		path = filepath.ToSlash(path)
		dependencies, err := forbiddenApplicationDependencies(path, nil)
		if err != nil {
			return err
		}
		for _, dependency := range dependencies {
			if _, ok := legacy[path][dependency]; ok {
				delete(legacy[path], dependency)
			} else {
				t.Errorf("%s uses forbidden application dependency %q", path, dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk application packages: %v", err)
	}
	for path, entries := range legacy {
		for dependency, reason := range entries {
			t.Errorf("remove stale application exception %s: %q (%s)", path, dependency, reason)
		}
	}
}

func forbiddenApplicationDependencies(path string, source any) ([]string, error) {
	checkTransport := strings.HasPrefix(path, "aigateway/") || strings.HasPrefix(path, "virtualization/consoleport/")
	mode := parser.ImportsOnly
	if checkTransport {
		mode = 0
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, mode)
	if err != nil {
		return nil, err
	}
	var violations []string
	httpAlias := ""
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return nil, err
		}
		if forbiddenApplicationImport(importPath) || checkTransport && forbiddenPortImport(importPath) {
			violations = append(violations, importPath)
		}
		if checkTransport && importPath == "net/http" {
			httpAlias = "http"
			if imported.Name != nil {
				httpAlias = imported.Name.Name
			}
		}
	}
	if httpAlias == "." {
		violations = append(violations, "net/http dot import")
	} else if httpAlias != "" {
		violations = append(violations, outboundHTTPDependencies(file, httpAlias)...)
	}
	return violations, nil
}

func forbiddenApplicationImport(path string) bool {
	for _, adapter := range []string{"infrastructure", "repository", "api", "bootstrap"} {
		root := "github.com/opensoha/soha/internal/" + adapter
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return strings.HasPrefix(path, "gorm.io/") || strings.HasPrefix(path, "k8s.io/") ||
		strings.HasPrefix(path, "helm.sh/") || strings.HasPrefix(path, "sigs.k8s.io/kustomize/") ||
		path == "sigs.k8s.io/controller-runtime" || strings.HasPrefix(path, "sigs.k8s.io/controller-runtime/")
}

func forbiddenPortImport(path string) bool {
	switch path {
	case "crypto/tls", "github.com/gin-gonic/gin", "github.com/gorilla/websocket", "golang.org/x/net/websocket":
		return true
	}
	return false
}

func outboundHTTPDependencies(file *ast.File, alias string) []string {
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name, ok := selector.X.(*ast.Ident)
		if !ok || name.Name != alias || name.Obj != nil {
			return true
		}
		switch selector.Sel.Name {
		case "Client", "DefaultClient", "DefaultTransport", "Transport", "RoundTripper", "NewRequest", "NewRequestWithContext", "Get", "Head", "Post", "PostForm":
			dependency := "net/http." + selector.Sel.Name
			if !slices.Contains(violations, dependency) {
				violations = append(violations, dependency)
			}
		}
		return true
	})
	return violations
}

func TestApplicationBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		name, path, source string
		want               []string
	}{
		{"personal environment port", "personalenvironment/service.go", `import "github.com/opensoha/soha/internal/domain/copilot"`, nil},
		{"personal environment adapter", "personalenvironment/service.go", `import "github.com/opensoha/soha/internal/repository/copilot"`, []string{"github.com/opensoha/soha/internal/repository/copilot"}},
		{"relay transport data", "aigateway/relayport/transport.go", `import "net/http"; type Response = http.Response`, nil},
		{"relay client", "aigateway/relayport/transport.go", `import wire "net/http"; var client wire.Client`, []string{"net/http.Client"}},
		{"shadowed relay identifier", "aigateway/relayport/transport.go", `import wire "net/http"; func f(wire struct{ Client string }) string { return wire.Client }; type Header = wire.Header`, nil},
		{"console websocket", "virtualization/consoleport/console.go", `import "github.com/gorilla/websocket"`, []string{"github.com/gorilla/websocket"}},
		{"worker SDK", "virtualization/worker_pool.go", `import "k8s.io/apimachinery/pkg/util/validation"`, []string{"k8s.io/apimachinery/pkg/util/validation"}},
		{"YAML codec", "resource/manifest.go", `import "sigs.k8s.io/yaml"`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := forbiddenApplicationDependencies(tc.path, "package fixture; "+tc.source)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("dependencies = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}
