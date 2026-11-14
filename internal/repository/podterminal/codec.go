package podterminal

import (
	"encoding/json"
	"fmt"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type protectedLease struct {
	Version                                    int
	ConnectionHash                             string
	ID, ActorID, CredentialHash, AccessTokenID string
	AuthzVersion                               int64
	CreatedAt, TicketExpiresAt, DeadlineAt     time.Time
	Principal                                  domainidentity.Principal
	Access                                     domainidentity.AccessContext
	Input                                      contractresource.PodTerminalInput
	Lease                                      contractresource.PodTerminalLease
}

func (repository *Repository) encode(record domainresource.PodTerminalRecord) (leaseRow, error) {
	lease := record.Lease
	lease.CreatedAt = lease.CreatedAt.UTC().Truncate(time.Microsecond)
	lease.TicketExpiresAt = lease.TicketExpiresAt.UTC().Truncate(time.Microsecond)
	lease.DeadlineAt = lease.DeadlineAt.UTC().Truncate(time.Microsecond)
	// Access metadata is not needed to revalidate this snapshot. Excluding it
	// also prevents unrelated credential metadata from entering the lease.
	record.Access.Metadata = nil
	row := leaseRow{ID: lease.ID, ActorID: record.ActorID, CredentialHash: record.CredentialHash, TicketHash: record.TicketHash, Status: lease.Status, CreatedAt: lease.CreatedAt, TicketExpiresAt: lease.TicketExpiresAt, DeadlineAt: lease.DeadlineAt, ClosedAt: lease.ClosedAt}
	// Authorization IDs deliberately use explicit fields: the identity JSON
	// view excludes them, but continuous authorization must retain their epoch.
	payload := protectedLease{Version: 1, ConnectionHash: record.ConnectionHash, ID: row.ID, ActorID: row.ActorID, CredentialHash: row.CredentialHash, AccessTokenID: record.Principal.AccessTokenID, AuthzVersion: record.Access.AuthzVersion, CreatedAt: row.CreatedAt, TicketExpiresAt: row.TicketExpiresAt, DeadlineAt: row.DeadlineAt, Principal: record.Principal, Access: record.Access, Input: record.Input, Lease: lease}
	data, err := json.Marshal(payload)
	if err != nil {
		return leaseRow{}, fmt.Errorf("encode protected terminal lease")
	}
	row.EncryptedPayload, err = secretcrypto.EncryptStringWithKeyring(repository.keys, string(data))
	if err != nil {
		return leaseRow{}, fmt.Errorf("encrypt protected terminal lease")
	}
	return row, nil
}

func (repository *Repository) decode(row leaseRow) (domainresource.PodTerminalRecord, error) {
	var payload protectedLease
	if !secretcrypto.Encrypted(row.EncryptedPayload) {
		return domainresource.PodTerminalRecord{}, fmt.Errorf("protected terminal lease is unavailable")
	}
	data, err := secretcrypto.DecryptStringWithKeyring(repository.keys, row.EncryptedPayload)
	if err != nil {
		return domainresource.PodTerminalRecord{}, fmt.Errorf("decrypt protected terminal lease")
	}
	if json.Unmarshal([]byte(data), &payload) != nil || !payload.matches(row) {
		return domainresource.PodTerminalRecord{}, fmt.Errorf("protected terminal lease binding does not match")
	}
	payload.Principal.AccessTokenID = payload.AccessTokenID
	payload.Access.AuthzVersion = payload.AuthzVersion
	lease := payload.Lease
	lease.Status, lease.ClosedAt = row.Status, row.ClosedAt
	if lease.Status == "pending" && !lease.TicketExpiresAt.After(time.Now()) {
		lease.Status = "expired"
		lease.ClosedAt = &lease.TicketExpiresAt
	}
	if (lease.Status == "pending" || lease.Status == "active" || lease.Status == "closing") && !lease.DeadlineAt.After(time.Now()) {
		lease.Status = "expired"
		lease.ClosedAt = &lease.DeadlineAt
	}
	return domainresource.PodTerminalRecord{ActorID: row.ActorID, CredentialHash: row.CredentialHash, TicketHash: row.TicketHash, ConnectionHash: payload.ConnectionHash, Principal: payload.Principal, Access: payload.Access, Input: payload.Input, Lease: lease}, nil
}

func (payload protectedLease) matches(row leaseRow) bool {
	return payload.Version == 1 && payload.ID == row.ID && payload.ActorID == row.ActorID && payload.Principal.UserID == row.ActorID && payload.CredentialHash == row.CredentialHash && payload.CreatedAt.Equal(row.CreatedAt) && payload.TicketExpiresAt.Equal(row.TicketExpiresAt) && payload.DeadlineAt.Equal(row.DeadlineAt) && payload.Lease.ID == row.ID && payload.Lease.CreatedAt.Equal(row.CreatedAt) && payload.Lease.TicketExpiresAt.Equal(row.TicketExpiresAt) && payload.Lease.DeadlineAt.Equal(row.DeadlineAt)
}
