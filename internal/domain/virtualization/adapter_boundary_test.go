package virtualization

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

func TestAdapterContractDoesNotImportTransportImplementations(t *testing.T) {
	violations, err := domainAdapterTransportDependencies(nil)
	if err != nil {
		t.Fatalf("parse adapter.go: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("domain adapter contract contains console transport dependencies: %v", violations)
	}
}

func domainAdapterTransportDependencies(source any) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "adapter.go", source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		if path == "crypto/tls" || path == "net/http" {
			violations = append(violations, path)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			switch ident.Name {
			case "BackendHeaders", "BackendTLS", "ConsoleURLResult", "GetConsoleURL":
				violations = append(violations, ident.Name)
			}
		}
		return true
	})
	return violations, nil
}

func TestDomainAdapterTransportBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"business data and comment", "// BackendTLS belongs to the transport port.\ntype VM struct{ Name string }", nil},
		{"transport field", "type VM struct{ BackendTLS string }", []string{"BackendTLS"}},
		{"transport operation", "type Adapter interface{ GetConsoleURL() string }", []string{"GetConsoleURL"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domainAdapterTransportDependencies("package virtualization\n" + tc.source)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("transport dependencies = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}
