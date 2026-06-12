package handlers

import (
	"crypto/tls"
	"testing"

	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	infravirtualization "github.com/opensoha/soha/internal/infrastructure/virtualization"
)

func TestBackendWebSocketDialerUsesConsoleTLSOptions(t *testing.T) {
	result := infravirtualization.ConsoleURLResult{
		BackendTLSConfig:      &tls.Config{ServerName: "k8s.example"},
		InsecureSkipTLSVerify: true,
	}

	dialer := backendWebSocketDialer(result)
	if dialer.TLSClientConfig == nil {
		t.Fatalf("TLSClientConfig is nil")
	}
	if dialer.TLSClientConfig == result.BackendTLSConfig {
		t.Fatalf("TLSClientConfig should be cloned")
	}
	if dialer.TLSClientConfig.ServerName != "k8s.example" {
		t.Fatalf("ServerName = %q", dialer.TLSClientConfig.ServerName)
	}
	if !dialer.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify = false, want true")
	}
}

func TestMapOperationIncludesDerivedOperationState(t *testing.T) {
	state := &domainvirtualization.OperationState{Phase: "failed", Retryable: true}
	mapped := mapOperation(domainvirtualization.Task{
		ID:             "task-1",
		TaskKind:       "vm_action",
		Status:         "failed",
		OperationState: state,
	})

	if mapped["operationState"] != state {
		t.Fatalf("operationState = %#v, want %#v", mapped["operationState"], state)
	}
}
