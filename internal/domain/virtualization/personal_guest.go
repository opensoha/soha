package virtualization

import (
	"context"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

// A provider may execute only its fixed installer; no caller-controlled command
// or filesystem path is part of this port.
type PersonalGuestProvisioner interface {
	ProvisionPersonalGuest(context.Context, AdapterConnection, AdapterVM, guest.Provisioning) (guest.DataResult, error)
}

type PersonalGuestToolsInstaller interface {
	InstallPersonalGuestTools(context.Context, AdapterConnection, AdapterVM, guest.ToolsPlan) (guest.ToolsResult, error)
}
