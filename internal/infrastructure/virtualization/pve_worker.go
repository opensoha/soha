package virtualization

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// InspectWorkerTemplate only accepts the root-disk layout whose storage demand
// the existing capacity ledger can reserve. The guest verifies installed tools.
func (a *PVEAdapter) InspectWorkerTemplate(ctx context.Context, connection Connection, node, templateID string, diskGiB int) (string, error) {
	config, err := a.pveVMConfig(ctx, connection, node, templateID)
	if err != nil {
		return "", err
	}
	if err := validatePVEReservedClone(config, diskGiB); err != nil {
		return "", err
	}
	if intFromAny(config["template"]) != 1 || !pveCloudInitDrive(stringFromAny(config["ide2"])) {
		return "", invalidf("worker image must be a template with an ide2 cloud-init drive")
	}
	for key := range config {
		if strings.HasPrefix(key, "net") && key != "net0" {
			return "", invalidf("worker image supports only the configured net0 network")
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}
