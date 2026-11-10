package conditionalmutation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

func mutationCodecFixture(t *testing.T) (*Repository, domainresource.ConditionalMutationRecord) {
	t.Helper()
	key, err := keyring.NewKey("test", "test-only-conditional-receipt-key", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := domainresource.ConditionalMutationRecord{ID: "11111111-1111-4111-8111-111111111111", ActorID: "actor", CredentialID: "session:server-session", KeyHash: strings.Repeat("1", 64), InputHash: strings.Repeat("2", 64), Status: "running", CreatedAt: now, DeadlineAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour), Authorization: domainai.ExecutionAuthorization{ActorID: "actor", ApprovalID: "approved", Call: domainai.ToolInvocationRequest{ToolName: "k8s.resources.conditional.update", RequestID: "original", SessionID: "server-session"}}}
	return New(nil, ring), record
}

func TestConditionalMutationCodecProtectsDigestAndSessionBinding(t *testing.T) {
	repo, record := mutationCodecFixture(t)
	row, err := repo.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(row)
	if !secretcrypto.Encrypted(row.EncryptedPayload) || strings.Contains(string(public), record.InputHash) || strings.Contains(string(public), "server-session") {
		t.Fatal("receipt digest/session leaked outside encryption")
	}
	decoded, err := repo.decode(row)
	if err != nil || decoded.Authorization.Call.SessionID != "server-session" || decoded.InputHash != record.InputHash {
		t.Fatal("lost protected receipt binding", err)
	}
	for _, name := range []string{"actor", "id", "key", "plaintext"} {
		t.Run(name, func(t *testing.T) {
			changed := row
			switch name {
			case "actor":
				changed.ActorID = "other"
			case "id":
				changed.ID = "other"
			case "key":
				changed.KeyHash = "other"
			case "plaintext":
				changed.EncryptedPayload = "{}"
			}
			if _, err := repo.decode(changed); err == nil {
				t.Fatal("unbound receipt accepted")
			}
		})
	}
	record.Authorization.Call.Input = map[string]any{"content": "private-yaml-value"}
	if _, err := repo.encode(record); err == nil {
		t.Fatal("original YAML may not be copied into receipt")
	}
}

func TestConditionalMutationCodecRetainsUnknownAndExpiry(t *testing.T) {
	repo, record := mutationCodecFixture(t)
	record.DeadlineAt = time.Now().Add(-time.Second)
	row, err := repo.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := repo.decode(row)
	if err != nil || decoded.Status != "unknown" {
		t.Fatal("crashed attempt became replayable", err)
	}
	row.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := repo.decode(row); !errors.Is(err, apperrors.ErrGone) {
		t.Fatal("expired payload accepted", err)
	}
}
