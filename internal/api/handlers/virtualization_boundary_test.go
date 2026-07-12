package handlers

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestVirtualizationHandlerDoesNotImportInfrastructure(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "virtualization.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse virtualization.go: %v", err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", spec.Path.Value, err)
		}
		if strings.HasPrefix(path, "github.com/opensoha/soha/internal/infrastructure/") {
			t.Fatalf("virtualization handler imports infrastructure package %q", path)
		}
	}
}
