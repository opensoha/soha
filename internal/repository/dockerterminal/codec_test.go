package dockerterminal

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	contractresource "github.com/opensoha/soha/contracts/resource"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

func codecRepository(t *testing.T) *Repository {
	t.Helper()
	key, err := keyring.NewKey("test", "test-only-terminal-key", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(nil, ring)
}

func codecRecord() domaindocker.NativeTerminalRecord {
	now := time.Now().UTC().Truncate(time.Microsecond)
	input, _ := (protocol.TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "main"}).Normalized()
	binding, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	return domaindocker.NativeTerminalRecord{HostID: "private-host", ServiceID: "private-service", HostRevision: "private-host-rev", ProjectRevision: "private-project-rev", EndpointDigest: strings.Repeat("3", 64), ActorID: "actor", CredentialHash: strings.Repeat("1", 64), TicketHash: strings.Repeat("2", 64), Principal: domainidentity.Principal{UserID: "actor", AccessTokenID: "private-token-id", PermissionKeys: []string{"private-permission"}}, Access: domainidentity.AccessContext{SessionID: "private-session", AuthzVersion: 7, ExpiresAt: now.Add(time.Hour), Metadata: map[string]any{"do-not-persist": "private-metadata"}}, Input: input, Lease: protocol.TerminalLease{Protocol: protocol.TerminalProtocol, ID: uuid.NewString(), Status: "pending", ProjectID: "private-project", Target: protocol.ContainerBinding{ProjectBinding: binding, ServiceName: input.ServiceName, ContainerID: input.ExpectedContainerID}, Shell: input.Shell, CreatedAt: now, TicketExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(5 * time.Minute), MaxInputBytes: contractresource.NativePodTerminalMaxInputBytes, MaxOutputBytes: contractresource.NativePodTerminalMaxOutputBytes}}
}

func TestNativeTerminalCodecEncryptsAndPreservesPrivateAuthorization(t *testing.T) {
	repository := codecRepository(t)
	record := codecRecord()
	row, err := repository.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(row)
	if !secretcrypto.Encrypted(row.EncryptedPayload) || strings.Contains(string(raw), "private-") {
		t.Fatal("lease snapshot persisted outside encryption")
	}
	decoded, err := repository.decode(row)
	if err != nil || decoded.Principal.AccessTokenID != record.Principal.AccessTokenID || decoded.Access.AuthzVersion != 7 || decoded.Lease.Target.ContainerID != record.Input.ExpectedContainerID || decoded.Access.Metadata != nil {
		t.Fatalf("authorization/target roundtrip: %+v err=%v", decoded, err)
	}
	cleartext, err := secretcrypto.DecryptStringWithKeyring(repository.keys, row.EncryptedPayload)
	if err != nil || strings.Contains(cleartext, "private-metadata") {
		t.Fatal("unrelated access metadata retained")
	}
	for _, field := range []string{"id", "actor", "credential", "created", "ticket-expiry", "deadline", "plaintext"} {
		t.Run(field, func(t *testing.T) {
			changed := row
			switch field {
			case "id":
				changed.ID = uuid.NewString()
			case "actor":
				changed.ActorID = "other"
			case "credential":
				changed.CredentialHash = "other"
			case "created":
				changed.CreatedAt = changed.CreatedAt.Add(time.Second)
			case "ticket-expiry":
				changed.TicketExpiresAt = changed.TicketExpiresAt.Add(time.Second)
			case "deadline":
				changed.DeadlineAt = changed.DeadlineAt.Add(time.Second)
			case "plaintext":
				changed.EncryptedPayload = cleartext
			}
			if _, err := repository.decode(changed); err == nil {
				t.Fatal("accepted tampered or plaintext lease")
			}
		})
	}
}

func TestNativeTerminalCodecExpirationCannotInventExit(t *testing.T) {
	repository := codecRepository(t)
	for _, status := range []string{"pending", "active", "closing"} {
		t.Run(status, func(t *testing.T) {
			record := codecRecord()
			record.Lease.Status = status
			record.Lease.CreatedAt = time.Now().Add(-time.Hour)
			record.Lease.TicketExpiresAt = time.Now().Add(-time.Minute)
			record.Lease.DeadlineAt = time.Now().Add(-time.Second)
			row, err := repository.encode(record)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := repository.decode(row)
			if err != nil || decoded.Lease.Status != "expired" || decoded.Lease.Result != nil || decoded.Lease.ClosedAt == nil {
				t.Fatalf("expired lease=%+v err=%v", decoded.Lease, err)
			}
		})
	}
}
