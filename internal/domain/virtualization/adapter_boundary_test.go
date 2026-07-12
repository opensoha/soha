package virtualization

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestAdapterContractDoesNotImportTransportImplementations(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "adapter.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse adapter.go: %v", err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", spec.Path.Value, err)
		}
		if path == "crypto/tls" || path == "net/http" {
			t.Fatalf("domain adapter contract imports transport implementation %q", path)
		}
	}
	content, err := os.ReadFile("adapter.go")
	if err != nil {
		t.Fatalf("read adapter.go: %v", err)
	}
	for _, transportField := range []string{"BackendHeaders", "BackendTLS", "ConsoleURLResult"} {
		if strings.Contains(string(content), transportField) {
			t.Fatalf("domain adapter contract contains console transport field %q", transportField)
		}
	}
}
