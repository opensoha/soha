package podexecution

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

func codecRepository(t *testing.T) *Repository {
	t.Helper()
	key, err := keyring.NewKey("test", "test-only-encryption-key", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(nil, ring)
}
func codecRecord() domainresource.PodExecutionRecord {
	now := time.Now().UTC()
	input, _ := (contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"echo", "private-argv-marker"}, StdinBase64: "cHJpdmF0ZS1zdGRpbi1tYXJrZXI="}).Normalized()
	return domainresource.PodExecutionRecord{ActorID: "actor", KeyHash: strings.Repeat("1", 64), InputHash: strings.Repeat("2", 64), ConnectionHash: strings.Repeat("c", 64), Input: input, Receipt: contractresource.PodExecutionReceipt{ID: "11111111-1111-4111-8111-111111111111", ClusterID: "cluster", Namespace: "test", PodName: "pod", ExpectedUID: "reviewed", Container: "main", Status: "running", CreatedAt: now, DeadlineAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}}
}
func TestPodExecutionCodecEncryptsAndBindsPayload(t *testing.T) {
	repository := codecRepository(t)
	record := codecRecord()
	row, err := repository.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if !secretcrypto.Encrypted(row.EncryptedPayload) || strings.Contains(row.EncryptedPayload, "private-") {
		t.Fatal("execution input was not encrypted")
	}
	public, _ := json.Marshal(row)
	if strings.Contains(string(public), record.InputHash) || strings.Contains(string(public), record.ConnectionHash) {
		t.Fatal("private input digest persisted outside encryption")
	}
	decoded, err := repository.decode(row)
	if err != nil || decoded.Input.Argv[1] != "private-argv-marker" || decoded.Input.StdinBase64 != record.Input.StdinBase64 || decoded.ConnectionHash != record.ConnectionHash {
		t.Fatalf("roundtrip failed: %v", err)
	}
	for _, field := range []string{"actor", "id", "key", "cluster", "namespace", "pod", "uid", "container", "plaintext"} {
		t.Run(field, func(t *testing.T) { podExecutionCodecEncryptsAndBindsPayloadPhase1(t, &repository, &row, &field) })
	}
}
func TestPodExecutionCodecPreservesUncertaintyAndExpiry(t *testing.T) {
	repository := codecRepository(t)
	record := codecRecord()
	record.Receipt.DeadlineAt = time.Now().Add(-time.Minute)
	row, err := repository.encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := repository.decode(row)
	if err != nil || decoded.Receipt.Status != "unknown" || decoded.Receipt.OutputAvailable || decoded.Receipt.FinishedAt == nil {
		t.Fatalf("expired in-flight receipt: %+v %v", decoded.Receipt, err)
	}
	row.ExpiresAt = time.Now().Add(-time.Minute)
	row.EncryptedPayload = ""
	if _, err := repository.decode(row); !errors.Is(err, apperrors.ErrGone) {
		t.Fatalf("expired tombstone became reusable: %v", err)
	}
}

func podExecutionCodecEncryptsAndBindsPayloadPhase1(t *testing.T, sharedRepository **Repository, sharedRow *executionRow, sharedField *string) {

	changed := (*sharedRow)
	switch *sharedField {
	case "actor":
		changed.ActorID = "other"
	case "id":
		changed.ID = "other"
	case "key":
		changed.KeyHash = "other"
	case "cluster":
		changed.ClusterID = "other"
	case "namespace":
		changed.Namespace = "other"
	case "pod":
		changed.PodName = "other"
	case "uid":
		changed.ExpectedUID = "other"
	case "container":
		changed.Container = "other"
	case "plaintext":
		data, _ := secretcrypto.DecryptStringWithKeyring((*sharedRepository).keys, (*sharedRow).EncryptedPayload)
		changed.EncryptedPayload = data
	}
	if _, err := (*sharedRepository).decode(changed); err == nil {
		t.Fatal("unbound or plaintext execution payload accepted")
	}
}
