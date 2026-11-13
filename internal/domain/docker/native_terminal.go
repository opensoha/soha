package docker

import (
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

// NativeTerminalRecord is sealed at rest. The endpoint is only a digest and
// the Agent bearer, ticket, stream output and keystrokes are never persisted.
type NativeTerminalRecord struct {
	ActorID, CredentialHash, TicketHash                              string
	HostID, ServiceID, HostRevision, ProjectRevision, EndpointDigest string
	Principal                                                        domainidentity.Principal
	Access                                                           domainidentity.AccessContext
	Input                                                            protocol.TerminalInput
	Lease                                                            protocol.TerminalLease
}
