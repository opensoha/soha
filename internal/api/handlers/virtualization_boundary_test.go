package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
)

func TestVirtualizationHandlerDoesNotImportInfrastructure(t *testing.T) {
	paths, err := filepath.Glob("virtualization*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		violations, err := virtualizationHandlerTransportDependencies(path, nil)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if len(violations) > 0 {
			t.Errorf("%s uses forbidden handler dependencies: %v", path, violations)
		}
	}
}

func virtualizationHandlerTransportDependencies(path string, source any) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		return nil, err
	}
	var violations []string
	websocketAlias := ""
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		if importPath == "crypto/tls" ||
			strings.HasPrefix(importPath+"/", "github.com/opensoha/soha/internal/infrastructure/") ||
			strings.HasPrefix(importPath+"/", "github.com/opensoha/soha/internal/repository/") {
			violations = append(violations, importPath)
		}
		if importPath == "github.com/gorilla/websocket" {
			websocketAlias = "websocket"
			if spec.Name != nil {
				websocketAlias = spec.Name.Name
			}
		}
	}
	if websocketAlias == "" {
		return violations, nil
	}
	if websocketAlias == "." {
		return append(violations, "websocket dot import"), nil
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name, ok := selector.X.(*ast.Ident)
		if !ok || name.Name != websocketAlias || name.Obj != nil {
			return true
		}
		switch selector.Sel.Name {
		case "Dialer", "DefaultDialer", "NewClient":
			violations = append(violations, "websocket."+selector.Sel.Name)
		}
		return true
	})
	return violations, nil
}

func TestVirtualizationHandlerTransportBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"browser upgrade", `import "github.com/gorilla/websocket"; var browser websocket.Upgrader`, nil},
		{"upstream dial", `import wire "github.com/gorilla/websocket"; var dialer = wire.DefaultDialer`, []string{"websocket.DefaultDialer"}},
		{"upstream TLS", `import "crypto/tls"; var config tls.Config`, []string{"crypto/tls"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := virtualizationHandlerTransportDependencies("virtualization.go", "package handlers; "+tc.source)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("transport dependencies = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestVirtualizationHandlerConsumerInterfacesRemainNarrow(t *testing.T) {
	assertNarrowHandlerCapabilities(t, reflect.TypeFor[VirtualizationHandler](), reflect.TypeFor[consoleport.Dialer]())
}

func assertNarrowHandlerCapabilities(t *testing.T, handler, callback reflect.Type) {
	t.Helper()
	for field := range handler.Fields() {
		if field.Type != callback && !isNarrowHandlerCapability(field.Type) {
			t.Errorf("%s.%s consumes %s; want a capability interface with 1–7 methods", handler, field.Name, field.Type)
		}
	}
}

func isNarrowHandlerCapability(capability reflect.Type) bool {
	return capability.Kind() == reflect.Interface && capability.NumMethod() > 0 && capability.NumMethod() <= 7
}

func TestHandlerCapabilityBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		capability reflect.Type
		want       bool
	}{
		{reflect.TypeFor[VirtualizationVMService](), true},
		{reflect.TypeFor[VirtualizationService](), false},
		{reflect.TypeFor[*VirtualizationHandler](), false},
		{reflect.TypeFor[any](), false},
	} {
		if got := isNarrowHandlerCapability(tc.capability); got != tc.want {
			t.Errorf("isNarrowHandlerCapability(%s) = %v, want %v", tc.capability, got, tc.want)
		}
	}
}
