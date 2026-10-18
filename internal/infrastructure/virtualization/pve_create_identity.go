package virtualization

import (
	"context"
	"net/http"
	"strconv"
)

func (a *PVEAdapter) nextVMIDCandidate(ctx context.Context, connection Connection, rejected []string) (string, error) {
	suggested, err := a.nextVMID(ctx, connection)
	if err != nil || len(rejected) == 0 {
		return suggested, err
	}
	candidate, err := strconv.Atoi(suggested)
	if err != nil || candidate < 100 || candidate > 999999999 {
		return "", invalidf("pve next vmid is invalid")
	}
	var inventory pveDataEnvelope
	if err := a.do(ctx, connection, http.MethodGet, "/cluster/resources?type=vm", nil, &inventory); err != nil {
		return "", err
	}
	unavailable := make(map[string]bool, len(rejected)+len(inventory.Data))
	for _, id := range rejected {
		unavailable[id] = true
	}
	for _, vm := range inventory.Data {
		unavailable[stringFromAny(vm["vmid"])] = true
	}
	for remaining := len(unavailable) + 1; remaining > 0 && candidate <= 999999999; remaining-- {
		id := strconv.Itoa(candidate)
		if !unavailable[id] {
			return id, nil
		}
		candidate++
	}
	return "", invalidf("no unclaimed pve vmid is available")
}
