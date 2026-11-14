package environment

import (
	"errors"
	"regexp"
)

const PersonalMCPStdioHeader = "X-Soha-MCP-Environment"

var personalMCPAssetID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
var personalMCPVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// PersonalMCPStdioRequest binds a registered command to the original owner
// lease. Neither the caller nor the model supplies a command or Docker ID.
type PersonalMCPStdioRequest struct {
	Lease   Lease  `json:"lease"`
	AssetID string `json:"assetId"`
	Version string `json:"version"`
}

func (r PersonalMCPStdioRequest) Validate() error {
	if !ValidPersonalMCPStdioAssetRef(r.AssetID, r.Version) {
		return errors.New("invalid personal MCP asset reference")
	}
	return (Request{SchemaVersion: RequestVersion, Action: "inspect", Lease: r.Lease}).Validate()
}

func ValidPersonalMCPStdioAssetRef(id, version string) bool {
	return personalMCPAssetID.MatchString(id) && personalMCPVersion.MatchString(version)
}
