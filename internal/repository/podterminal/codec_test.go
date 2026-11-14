package podterminal

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	contractresource "github.com/opensoha/soha/contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
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

func codecRecord() domainresource.PodTerminalRecord {
	now := time.Now().UTC().Truncate(time.Microsecond)
	input, _ := (contractresource.PodTerminalInput{ExpectedUID: "private-pod-uid", Container: "main"}).Normalized()
	return domainresource.PodTerminalRecord{ActorID: "actor", CredentialHash: strings.Repeat("1", 64), TicketHash: strings.Repeat("2", 64), ConnectionHash: strings.Repeat("c", 64), Principal: domainidentity.Principal{UserID: "actor", AccessTokenID: "private-token-id", PermissionKeys: []string{"private-permission"}}, Access: domainidentity.AccessContext{SessionID: "private-session", AuthzVersion: 7, ExpiresAt: now.Add(time.Hour), Metadata: map[string]any{"do-not-persist": "private-metadata"}}, Input: input, Lease: contractresource.PodTerminalLease{ID: uuid.NewString(), Status: "pending", ClusterID: "private-cluster", Target: contractresource.PodExecutionTarget{Namespace: "test", PodName: "app", UID: input.ExpectedUID, Container: "main", CheckedAt: now}, Shell: input.Shell, CreatedAt: now, TicketExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(5 * time.Minute), MaxInputBytes: contractresource.NativePodTerminalMaxInputBytes, MaxOutputBytes: contractresource.NativePodTerminalMaxOutputBytes}}
}

func TestNativeTerminalCodecEncryptsAndPreservesPrivateAuthorization(t *testing.T) {
	repository := codecRepository(t)
	record := codecRecord()
	row, err := repository.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(row)
	if !secretcrypto.Encrypted(row.EncryptedPayload) || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), record.ConnectionHash) {
		t.Fatal("lease snapshot persisted outside encryption")
	}
	decoded, err := repository.decode(row)
	if err != nil || decoded.Principal.AccessTokenID != record.Principal.AccessTokenID || decoded.Access.AuthzVersion != 7 || decoded.Lease.Target.UID != record.Input.ExpectedUID || decoded.Access.Metadata != nil || decoded.ConnectionHash != record.ConnectionHash {
		t.Fatalf("authorization/target roundtrip: %+v err=%v", decoded, err)
	}
	cleartext, err := secretcrypto.DecryptStringWithKeyring(repository.keys, row.EncryptedPayload)
	if err != nil || strings.Contains(cleartext, "private-metadata") {
		t.Fatal("unrelated access metadata retained")
	}
	for _, field := range []string{"id", "actor", "credential", "created", "ticket-expiry", "deadline", "plaintext"} {
		t.Run(field, func(t *testing.T) {
			nativeTerminalCodecEncryptsAndPreservesPrivateAuthorizationPhase1(t, &repository, &row, &cleartext, &field)
		})
	}
}

func TestNativeTerminalCodecCanReadLegacyUnboundLease(t *testing.T) {
	repository := codecRepository(t)
	record := codecRecord()
	row, err := repository.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	cleartext, err := secretcrypto.DecryptStringWithKeyring(repository.keys, row.EncryptedPayload)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err = json.Unmarshal([]byte(cleartext), &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "ConnectionHash")
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	row.EncryptedPayload, err = secretcrypto.EncryptStringWithKeyring(repository.keys, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := repository.decode(row)
	if err != nil || decoded.ConnectionHash != "" || decoded.Lease.ID != record.Lease.ID {
		t.Fatalf("legacy metadata no longer readable: %v", err)
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

func nativeTerminalCodecEncryptsAndPreservesPrivateAuthorizationPhase1(t *testing.T, sharedRepository **Repository, sharedRow *leaseRow, sharedCleartext *string, sharedField *string) {

	changed := (*sharedRow)
	switch *sharedField {
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
		changed.EncryptedPayload = (*sharedCleartext)
	}
	if _, err := (*sharedRepository).decode(changed); err == nil {
		t.Fatal("accepted tampered or plaintext lease")
	}
}
