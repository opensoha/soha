package podexecution

import (
	"encoding/json"
	"fmt"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type protectedPayload struct {
	Version                                                                                int
	ConnectionHash                                                                         string
	ID, ActorID, KeyHash, InputHash, ClusterID, Namespace, PodName, ExpectedUID, Container string
	Input                                                                                  contractresource.PodArgvInput
	Output                                                                                 *contractresource.PodExecutionOutput
}

func (r *Repository) encode(record domainresource.PodExecutionRecord) (executionRow, error) {
	item := record.Receipt
	row := executionRow{ID: item.ID, ActorID: record.ActorID, KeyHash: record.KeyHash, ClusterID: item.ClusterID, Namespace: item.Namespace, PodName: item.PodName, ExpectedUID: item.ExpectedUID, Container: item.Container, Status: item.Status, CreatedAt: item.CreatedAt, DeadlineAt: item.DeadlineAt, ExpiresAt: item.ExpiresAt, FinishedAt: item.FinishedAt}
	// The original-input digest is encrypted too: a plain digest would expose
	// low-entropy argv/stdin to offline guessing, even after payload cleanup.
	payload := protectedPayload{Version: 1, ConnectionHash: record.ConnectionHash, ID: row.ID, ActorID: row.ActorID, KeyHash: row.KeyHash, InputHash: record.InputHash, ClusterID: row.ClusterID, Namespace: row.Namespace, PodName: row.PodName, ExpectedUID: row.ExpectedUID, Container: row.Container, Input: record.Input, Output: item.Output}
	data, err := json.Marshal(payload)
	if err != nil {
		return executionRow{}, fmt.Errorf("encode protected Pod execution")
	}
	row.EncryptedPayload, err = secretcrypto.EncryptStringWithKeyring(r.keys, string(data))
	if err != nil {
		return executionRow{}, fmt.Errorf("encrypt protected Pod execution")
	}
	return row, nil
}

func (r *Repository) decode(row executionRow) (domainresource.PodExecutionRecord, error) {
	var record domainresource.PodExecutionRecord
	if !row.ExpiresAt.After(time.Now()) {
		return record, apperrors.ErrGone
	}
	if !secretcrypto.Encrypted(row.EncryptedPayload) {
		return record, fmt.Errorf("protected Pod execution payload is unavailable")
	}
	data, err := secretcrypto.DecryptStringWithKeyring(r.keys, row.EncryptedPayload)
	if err != nil {
		return record, fmt.Errorf("decrypt protected Pod execution")
	}
	var payload protectedPayload
	if json.Unmarshal([]byte(data), &payload) != nil || !payload.matches(row) {
		return record, fmt.Errorf("protected Pod execution binding does not match")
	}
	receipt := contractresource.PodExecutionReceipt{ID: row.ID, Status: row.Status, ClusterID: row.ClusterID, Namespace: row.Namespace, PodName: row.PodName, ExpectedUID: row.ExpectedUID, Container: row.Container, CreatedAt: row.CreatedAt, DeadlineAt: row.DeadlineAt, ExpiresAt: row.ExpiresAt, FinishedAt: row.FinishedAt, OutputAvailable: payload.Output != nil, Output: payload.Output}
	// A crashed Core cannot determine whether the process ran. Reading an
	// expired attempt returns unknown and never authorizes another attempt.
	if receipt.Status == "running" && !receipt.DeadlineAt.After(time.Now()) {
		receipt.Status = "unknown"
		receipt.FinishedAt = &receipt.DeadlineAt
	}
	return domainresource.PodExecutionRecord{ActorID: row.ActorID, KeyHash: row.KeyHash, InputHash: payload.InputHash, ConnectionHash: payload.ConnectionHash, Input: payload.Input, Receipt: receipt}, nil
}

func (payload protectedPayload) matches(row executionRow) bool {
	return payload.Version == 1 && payload.ID == row.ID && payload.ActorID == row.ActorID && payload.KeyHash == row.KeyHash && payload.ClusterID == row.ClusterID && payload.Namespace == row.Namespace && payload.PodName == row.PodName && payload.ExpectedUID == row.ExpectedUID && payload.Container == row.Container
}
