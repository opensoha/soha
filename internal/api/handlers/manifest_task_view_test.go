package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	appmanifest "github.com/opensoha/soha/internal/application/manifest"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
)

type manifestTaskViewSource struct {
	ManifestSourceService
	task domaindelivery.ExecutionTask
}

func (s manifestTaskViewSource) Preflight(context.Context, domainidentity.Principal, string, domainmanifest.PreflightInput) (domaindelivery.ExecutionTask, error) {
	return s.task, nil
}

func (s manifestTaskViewSource) Sync(context.Context, domainidentity.Principal, string, domainmanifest.SyncInput, string) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error) {
	return domainmanifest.SyncRun{}, s.task, nil
}

func (s manifestTaskViewSource) SyncWebhook(context.Context, domainidentity.Principal, string, domainmanifest.SyncWebhookInput) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error) {
	return domainmanifest.SyncRun{}, s.task, nil
}

func TestManifestUserTaskResponsesKeepRunnerCredentialsPrivate(t *testing.T) {
	task := domaindelivery.ExecutionTask{ID: "task-1", ApplicationID: "app-1", Status: "queued", TaskKind: "manifest_apply", CallbackToken: "manifest-synthetic-callback", SecretLease: &domainsecret.LeaseGrant{Token: "manifest-synthetic-lease"}}
	handler := NewManifestSourceHandler(manifestTaskViewSource{task: task})
	for name, action := range map[string]func(*gin.Context){"preflight": handler.Preflight, "sync": handler.Sync, "webhook": handler.SyncWebhook} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(`{}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			action(ctx)
			if response.Code != http.StatusAccepted {
				t.Fatalf("handler status=%d", response.Code)
			}
			assertManifestPublicTask(t, response.Body.Bytes(), name != "preflight")
		})
	}
	view := manifestDeploymentActionDTO(appmanifest.DeploymentActionResult{Task: task})
	raw, err := json.Marshal(map[string]any{"data": view})
	if err != nil {
		t.Fatal(err)
	}
	assertManifestPublicTask(t, raw, true)
	raw, err = json.Marshal(task)
	if err != nil || !bytes.Contains(raw, []byte(task.CallbackToken)) || !bytes.Contains(raw, []byte(task.SecretLease.Token)) || task.Payload != nil {
		t.Fatal("user response projection changed the original runner task")
	}
}

func assertManifestPublicTask(t *testing.T, raw []byte, nested bool) {
	t.Helper()
	if bytes.Contains(raw, []byte("manifest-synthetic-")) {
		t.Fatal("manifest user response exposed a runner credential")
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	task := envelope.Data
	if nested {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(task["task"], &inner); err != nil {
			t.Fatal(err)
		}
		task = inner
	}
	if string(task["callbackToken"]) != `"[redacted]"` || string(task["payload"]) != `{}` || string(task["status"]) != `"queued"` || task["secretLease"] != nil {
		t.Fatal("public manifest task shape or accepted lifecycle state changed")
	}
}
