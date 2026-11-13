package resource

import (
	contractresource "github.com/opensoha/soha/contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

// PodTerminalRecord is private lease state. The credential binding is a hash;
// the principal/access snapshot and input are encrypted at rest. No bearer,
// keystrokes or terminal output belong here.
type PodTerminalRecord struct {
	ActorID        string
	CredentialHash string
	TicketHash     string
	ConnectionHash string
	Principal      domainidentity.Principal
	Access         domainidentity.AccessContext
	Input          contractresource.PodTerminalInput
	Lease          contractresource.PodTerminalLease
}
