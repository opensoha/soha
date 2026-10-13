package virtualization

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

//go:embed personal_guest_installer.py
var personalGuestInstaller string

func (a *PVEAdapter) ProvisionPersonalGuest(ctx context.Context, connection Connection, vm VM, bundle guest.Provisioning) (guest.DataResult, error) {
	if bundle.Validate(time.Now()) != nil || vm.Node == "" || vm.ID == "" {
		return guest.DataResult{}, domain.ErrInvalid
	}
	if _, err := strconv.ParseUint(vm.ID, 10, 32); err != nil {
		return guest.DataResult{}, domain.ErrInvalid
	}
	raw, err := json.Marshal(bundle)
	if err != nil || len(raw) > 65536 {
		return guest.DataResult{}, domain.ErrInvalid
	}
	base := "/nodes/" + url.PathEscape(vm.Node) + "/qemu/" + url.PathEscape(vm.ID) + "/agent/"
	var dispatched struct {
		Data struct {
			PID int `json:"pid"`
		} `json:"data"`
	}
	if a.do(ctx, connection, http.MethodPost, base+"exec", map[string]any{"command": []string{"/usr/bin/nsenter", "--mount=/proc/1/ns/mnt", "--", "/usr/bin/python3", "-c", personalGuestInstaller}, "input-data": string(raw)}, &dispatched) != nil || dispatched.Data.PID < 1 {
		return guest.DataResult{}, fmt.Errorf("personal guest provider dispatch is unconfirmed")
	}
	return a.waitPersonalGuest(ctx, connection, base, dispatched.Data.PID, bundle.Plan)
}

func (a *PVEAdapter) waitPersonalGuest(ctx context.Context, connection Connection, base string, pid int, plan guest.DataPlan) (guest.DataResult, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var status struct {
			Data struct {
				Exited         int    `json:"exited"`
				ExitCode       int    `json:"exitcode"`
				Signal         int    `json:"signal"`
				Output         string `json:"out-data"`
				Truncated      int    `json:"out-truncated"`
				ErrorOutput    string `json:"err-data"`
				ErrorTruncated int    `json:"err-truncated"`
			} `json:"data"`
		}
		if a.do(ctx, connection, http.MethodGet, base+"exec-status?pid="+strconv.Itoa(pid), nil, &status) != nil {
			return guest.DataResult{}, fmt.Errorf("personal guest provider result is unconfirmed")
		}
		if status.Data.Exited == 1 {
			if status.Data.ExitCode != 0 || status.Data.Signal != 0 || status.Data.Truncated != 0 || len(status.Data.Output) > 8192 {
				return guest.DataResult{}, &personalGuestInstallerFailure{stage: personalGuestFailureStage(status.Data.ErrorOutput, status.Data.ErrorTruncated)}
			}
			var result guest.DataResult
			if json.Unmarshal([]byte(status.Data.Output), &result) != nil || result.DataRef != plan.Binding.DataRef || result.FilesystemID != result.DataRef || result.DiskQuota != plan.DiskQuotaBytes || result.DiskUsed < 0 || result.DiskUsed > result.DiskQuota {
				return result, fmt.Errorf("personal guest data receipt is invalid")
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			return guest.DataResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Only fixed installer stages may leave the privileged boundary.
type personalGuestInstallerFailure struct{ stage string }

func (e *personalGuestInstallerFailure) Error() string {
	return "personal guest installer failed; retain the original operation"
}
func (e *personalGuestInstallerFailure) ProvisioningStage() string { return e.stage }

func personalGuestFailureStage(output string, truncated int) string {
	if truncated != 0 || len(output) > 512 {
		return "unknown"
	}
	output = strings.TrimSuffix(output, "\n")
	stage := strings.TrimPrefix(output, "managed personal guest provisioning failed stage=")
	stage = strings.TrimSuffix(stage, "; retain the original operation")
	switch stage {
	case "validation", "artifact", "packages", "service", "registration", "runtime_scope", "data_initialization", "project_namespace", "long_service":
		if output == "managed personal guest provisioning failed stage="+stage+"; retain the original operation" {
			return stage
		}
	}
	return "unknown"
}
