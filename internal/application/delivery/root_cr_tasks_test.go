package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type rootClaimValidator struct {
	ManifestDelivery
	err   error
	calls int
}

type rootLogRepository struct {
	stubRepository
	read bool
}

func (r *rootLogRepository) GetExecutionTask(context.Context, string) (dd.ExecutionTask, error) {
	return dd.ExecutionTask{ApplicationID: "app", Payload: map[string]any{"rootResource": map[string]any{"uid": "root"}}}, nil
}
func (r *rootLogRepository) ListExecutionLogs(context.Context, string, int) ([]dd.ExecutionLog, error) {
	r.read = true
	return []dd.ExecutionLog{{Message: "synthetic-private-diagnostic"}}, nil
}
func TestRootCRExecutionLogsRetainSensitiveDataPermission(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		repo := &rootLogRepository{}
		keys := []string{appaccess.PermDeliveryExecutionTasksView}
		if allowed {
			keys = append(keys, appaccess.PermPlatformConfigurationSecretDataView)
		}
		service := &Service{repository: repo, applications: stubApplicationReader{}, permissions: deliveryActionPermissions(keys...)}
		_, err := service.ListExecutionLogs(t.Context(), deliveryActionPrincipal(), "task", 10)
		if allowed && err != nil || !allowed && !errors.Is(err, apperrors.ErrAccessDenied) || repo.read != allowed {
			t.Fatalf("sensitive logs: allowed=%v read=%v error=%v", allowed, repo.read, err)
		}
	}
}

func (r *rootClaimValidator) ValidateRootCRExecutionTask(context.Context, dd.ExecutionTask) error {
	r.calls++
	return r.err
}
func TestRootCRClaimRequiresApprovedFrozenPlanAndCurrentPermission(t *testing.T) {
	for _, mode := range []string{"apply", "confirming", "approval-missing", "wrong-document", "wrong-root", "wrong-app", "unconfirmed", "permission", "adopt"} {
		t.Run(mode, func(t *testing.T) {
			root := &sohaapi.ManifestRootResource{UID: "uid", Generation: 2, DeliveryPlanID: "plan", TargetID: "target"}
			snapshot := dm.DeliverySnapshot{RootResource: root, DeliveryPlanID: "plan", TargetID: "target", PackageID: "package", BindingID: "binding", ClusterID: "cluster", Namespace: "team", RenderedDigest: "digest", Documents: []dm.RenderedDocument{{Content: "frozen"}}}
			plan := dd.DeliveryPlan{ID: "plan", Status: dd.DeliveryPlanStatusConfirmed, ApplicationID: "app", ApplicationEnvironmentID: "env", RequiresApproval: true, Impact: map[string]any{"approval": []any{map[string]any{"status": "approved"}}}, ManifestSnapshots: []dm.DeliverySnapshot{snapshot}}
			payload := dm.TaskPayload{RootResource: root, Action: "apply", PackageID: "package", BindingID: "binding", ClusterID: "cluster", Namespace: "team", RenderedDigest: "digest", Documents: snapshot.Documents}
			validator := &rootClaimValidator{}
			switch mode {
			case "confirming":
				plan.Status = dd.DeliveryPlanStatusConfirming
			case "approval-missing":
				plan.Impact = nil
			case "unconfirmed":
				plan.Status = dd.DeliveryPlanStatusDraft
			case "wrong-document":
				payload.Documents = []dm.RenderedDocument{{Content: "changed"}}
			case "wrong-root":
				copy := *root
				copy.UID = "replacement"
				payload.RootResource = &copy
			case "wrong-app":
				plan.ApplicationID = "other"
			case "permission":
				validator.err = apperrors.ErrAccessDenied
			case "adopt":
				payload.Action = "adopt"
			}
			data, _ := json.Marshal(payload)
			wire := map[string]any{}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			task := dd.ExecutionTask{ApplicationID: "app", ApplicationEnvironmentID: "env", TaskKind: "manifest_apply", Payload: wire}
			service := &Service{repository: &planRepository{plan: plan}, manifestDelivery: validator}
			_, err := service.HydrateExecutionTask(t.Context(), task)
			valid := mode == "apply" || mode == "confirming"
			if valid && err != nil || !valid && err == nil {
				t.Fatalf("claim=%v valid=%v", err, valid)
			}
			if mode == "permission" && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatal(err)
			}
		})
	}
}
