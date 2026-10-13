package copilot

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestNativeToolCannotOutliveRemainingRunTimeBudget(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	cp := initialNativeCheckpoint(store.lease.Run)
	cp.Budget.Deadline = time.Now().Add(2 * time.Second)
	cp.Pending = []nativeCall{{ToolName: "knowledge.search", State: "prepared", Call: appaigateway.WorkbenchToolCall{ID: "read", Arguments: json.RawMessage(`{"query":"receipt"}`)}}}
	principal, err := service.nativePrincipal(t.Context(), store.lease.Run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.nativeToolStep(t.Context(), store, store.lease, &cp, principal); err != nil || gateway.toolDeadline.IsZero() || gateway.toolDeadline.After(cp.Budget.Deadline) {
		t.Fatalf("tool exceeded remaining run budget: deadline=%v parent=%v err=%v", gateway.toolDeadline, cp.Budget.Deadline, err)
	}
}

func TestNativeBudgetAccountsMissingEstimatedAndInvalidUsage(t *testing.T) {
	for _, kind := range []string{"known", "missing", "estimated", "negative", "nan", "infinity", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			_, store, _, _ := nativeLoopFixture(t)
			cp := initialNativeCheckpoint(store.lease.Run)
			cp.Usage.Tokens, cp.BudgetTokens = 2048, 2048
			request := appaigateway.WorkbenchRelayRequest{Messages: []appaigateway.WorkbenchRelayMessage{{Role: "user", Content: "evidence"}}}
			if err := reserveNativeModelBudget(&cp, &request); err != nil {
				t.Fatal(err)
			}
			reserved := cp.ModelReservation
			response := appaigateway.WorkbenchRelayResponse{Usage: map[string]float64{"totalTokens": 23}}
			switch kind {
			case "missing":
				response.Usage = nil
			case "estimated":
				response.UsageEstimated = true
			case "negative":
				response.Usage["totalTokens"] = -1
			case "nan":
				response.Usage["totalTokens"] = math.NaN()
			case "infinity":
				response.Usage["totalTokens"] = math.Inf(1)
			case "overflow":
				response.Usage["totalTokens"] = math.Nextafter(float64(math.MaxInt64), 0)
			}
			err := settleNativeModelBudget(&cp, response)
			valid := kind == "known" || kind == "missing" || kind == "estimated"
			if (err == nil) != valid {
				t.Fatalf("invalid usage was accepted: %v", err)
			}
			want := reserved + 2048
			if kind == "known" {
				want = 23 + 2048
			}
			if cp.BudgetTokens != want || cp.UsageKnown != (kind == "known") {
				t.Fatalf("accounting lost unknown reservation: %+v", cp)
			}
		})
	}
}

func TestNativeBudgetIncludesSchemasArgumentsAndLostModelAttempts(t *testing.T) {
	for _, kind := range []string{"schema", "tool_arguments", "lost_attempt"} {
		t.Run(kind, func(t *testing.T) {
			service, store, gateway, _ := nativeLoopFixture(t)
			cp := initialNativeCheckpoint(store.lease.Run)
			switch kind {
			case "schema":
				catalog, _ := gateway.WorkbenchCapabilities(t.Context(), domainidentity.Principal{}, domainaigateway.ManifestRequest{})
				gateway.catalog = catalog.Tools
				gateway.catalog[0].InputSchema["description"] = strings.Repeat("x", 32768)
			case "tool_arguments":
				cp.Messages = []appaigateway.WorkbenchRelayMessage{{Role: "assistant", ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "huge", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"` + strings.Repeat("x", 32768) + `"}`)}}}}
			case "lost_attempt":
				cp.BudgetTokens, cp.ModelReservation, cp.Usage.Steps = 32700, 32000, 1
				store.lease.Phase = "model"
			}
			var err error
			store.lease.Checkpoint, err = json.Marshal(cp)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				calls++
				return appaigateway.WorkbenchRelayResponse{Content: "must not run"}, nil
			}
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || calls != 0 || store.lease.Run.Status != "failed" || len(store.final) != 1 || store.final[0].Role != "system" {
				t.Fatalf("budget allowed another request: calls=%d status=%s err=%v", calls, store.lease.Run.Status, err)
			}
			if kind == "lost_attempt" && store.lease.Run.Output["usageKnown"] != false {
				t.Fatal("lost attempt became exact usage")
			}
		})
	}
}
