package docker

import (
	"errors"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestSafeTableNameAllowsKnownDockerTables(t *testing.T) {
	for _, tableName := range []string{
		"docker_hosts",
		"docker_projects",
		"docker_services",
		"docker_port_mappings",
		"docker_templates",
		"docker_operations",
	} {
		got, err := safeTableName(tableName)
		if err != nil {
			t.Fatalf("safeTableName(%q) error = %v", tableName, err)
		}
		if got != tableName {
			t.Fatalf("safeTableName(%q) = %q", tableName, got)
		}
	}
}

func TestSafeTableNameRejectsUnknownDockerTables(t *testing.T) {
	if _, err := safeTableName("docker_hosts; DROP TABLE docker_hosts"); err == nil {
		t.Fatal("safeTableName accepted unsafe table name")
	}
}

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatal("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}
