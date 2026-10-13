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

//go:embed personal_guest_tools_installer.py
var personalGuestToolsBody string

func (a *PVEAdapter) InstallPersonalGuestTools(ctx context.Context, connection Connection, vm VM, plan guest.ToolsPlan) (guest.ToolsResult, error) {
	raw, err := guestToolsDispatchPayload(vm, plan)
	if err != nil {
		return guest.ToolsResult{}, err
	}
	// Reuse only the fixed installer's file/TLS/tool helpers, not its data initializer.
	helpers, _, found := strings.Cut(personalGuestInstaller, "STAGE = \"validation\"")
	if !found {
		return guest.ToolsResult{}, domain.ErrInvalid
	}
	base := "/nodes/" + url.PathEscape(vm.Node) + "/qemu/" + url.PathEscape(vm.ID) + "/agent/"
	var dispatched struct {
		Data struct {
			PID int `json:"pid"`
		} `json:"data"`
	}
	if a.do(ctx, connection, http.MethodPost, base+"exec", map[string]any{"command": []string{"/usr/bin/nsenter", "--mount=/proc/1/ns/mnt", "--", "/usr/bin/python3", "-c", helpers + personalGuestToolsBody}, "input-data": string(raw)}, &dispatched) != nil || dispatched.Data.PID < 1 {
		return guest.ToolsResult{}, fmt.Errorf("personal guest tools dispatch is unconfirmed")
	}
	ctx, cancel := context.WithTimeout(ctx, 14*time.Minute)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var status struct {
			Data struct {
				Exited    int    `json:"exited"`
				ExitCode  int    `json:"exitcode"`
				Signal    int    `json:"signal"`
				Output    string `json:"out-data"`
				Truncated int    `json:"out-truncated"`
			} `json:"data"`
		}
		if a.do(ctx, connection, http.MethodGet, base+"exec-status?pid="+strconv.Itoa(dispatched.Data.PID), nil, &status) != nil {
			return guest.ToolsResult{}, fmt.Errorf("personal guest tools result is unconfirmed")
		}
		if status.Data.Exited == 1 {
			var result guest.ToolsResult
			if status.Data.ExitCode != 0 || status.Data.Signal != 0 || status.Data.Truncated != 0 || len(status.Data.Output) > 2048 || json.Unmarshal([]byte(status.Data.Output), &result) != nil || result.OperationID != plan.OperationID || result.DataRef != plan.Binding.DataRef || result.ArtifactSHA256 != plan.ArtifactSHA256 {
				return guest.ToolsResult{}, fmt.Errorf("personal guest tools installer failed; retain the operation")
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			return guest.ToolsResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func guestToolsDispatchPayload(vm VM, plan guest.ToolsPlan) ([]byte, error) {
	if plan.Validate(time.Now()) != nil || vm.Node == "" {
		return nil, domain.ErrInvalid
	}
	if _, err := strconv.ParseUint(vm.ID, 10, 32); err != nil {
		return nil, domain.ErrInvalid
	}
	raw, err := json.Marshal(plan)
	if err != nil || len(raw) > 8192 {
		return nil, domain.ErrInvalid
	}
	return raw, nil
}
