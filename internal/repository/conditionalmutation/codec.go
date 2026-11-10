package conditionalmutation

import (
	"encoding/json"
	"fmt"
	"time"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type protectedPayload struct {
	Version                                                                  int
	ID, ActorID, KeyHash, CredentialID, InputHash, SessionID, ConnectionHash string
	Authorization                                                            domainai.ExecutionAuthorization
	Output                                                                   map[string]any
}

func (r *Repository) encode(record domainresource.ConditionalMutationRecord) (mutationRow, error) {
	row := mutationRow{ID: record.ID, ActorID: record.ActorID, KeyHash: record.KeyHash, Status: record.Status,
		CreatedAt: record.CreatedAt, DeadlineAt: record.DeadlineAt, ExpiresAt: record.ExpiresAt, FinishedAt: record.FinishedAt}
	if record.Authorization.Call.Input != nil || record.Authorization.ActorID != record.ActorID || authorizationCredentialID(record.Authorization) != record.CredentialID || record.CredentialID == "" || record.Authorization.ApprovalID == "" {
		return mutationRow{}, apperrors.ErrInvalidArgument
	}
	payload := protectedPayload{Version: 1, ID: record.ID, ActorID: record.ActorID, KeyHash: record.KeyHash, CredentialID: record.CredentialID, InputHash: record.InputHash, SessionID: record.Authorization.Call.SessionID, ConnectionHash: record.ConnectionHash, Authorization: record.Authorization, Output: record.Output}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 64<<10 {
		return mutationRow{}, fmt.Errorf("invalid protected conditional mutation receipt")
	}
	row.EncryptedPayload, err = secretcrypto.EncryptStringWithKeyring(r.keys, string(raw))
	if err != nil {
		return mutationRow{}, fmt.Errorf("encrypt protected conditional mutation receipt")
	}
	return row, nil
}

func (r *Repository) decode(row mutationRow) (domainresource.ConditionalMutationRecord, error) {
	var record domainresource.ConditionalMutationRecord
	if !row.ExpiresAt.After(time.Now()) {
		return record, apperrors.ErrGone
	}
	if !secretcrypto.Encrypted(row.EncryptedPayload) {
		return record, fmt.Errorf("protected conditional mutation receipt is unavailable")
	}
	raw, err := secretcrypto.DecryptStringWithKeyring(r.keys, row.EncryptedPayload)
	if err != nil {
		return record, fmt.Errorf("decrypt protected conditional mutation receipt")
	}
	var payload protectedPayload
	if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &payload) != nil {
		return record, fmt.Errorf("protected conditional mutation receipt binding differs")
	}
	payload.Authorization.Call.SessionID = payload.SessionID
	if !payload.matches(row) {
		return record, fmt.Errorf("protected conditional mutation receipt binding differs")
	}
	record = domainresource.ConditionalMutationRecord{ID: row.ID, ActorID: row.ActorID, KeyHash: row.KeyHash, CredentialID: payload.CredentialID, InputHash: payload.InputHash, ConnectionHash: payload.ConnectionHash, Authorization: payload.Authorization, Output: payload.Output,
		Status: row.Status, CreatedAt: row.CreatedAt, DeadlineAt: row.DeadlineAt, ExpiresAt: row.ExpiresAt, FinishedAt: row.FinishedAt}
	if record.Status == "running" && !record.DeadlineAt.After(time.Now()) {
		record.Status = "unknown"
	}
	return record, nil
}

func (p protectedPayload) matches(row mutationRow) bool {
	return p.Version == 1 && p.ID == row.ID && p.ActorID == row.ActorID && p.KeyHash == row.KeyHash && p.Authorization.Call.Input == nil && p.Authorization.ActorID == row.ActorID && p.CredentialID != "" && authorizationCredentialID(p.Authorization) == p.CredentialID && p.Authorization.ApprovalID != ""
}

func authorizationCredentialID(authorization domainai.ExecutionAuthorization) string {
	if authorization.ActorTokenID != "" {
		return "token:" + authorization.ActorTokenID
	}
	if authorization.Call.SessionID != "" {
		return "session:" + authorization.Call.SessionID
	}
	return ""
}
