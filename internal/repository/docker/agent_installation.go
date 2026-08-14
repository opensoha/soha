package docker

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) CreateHostAgentInstallation(ctx context.Context, operationInput domaindocker.OperationInput, state domaindocker.HostAgentInstallationState) (domaindocker.Operation, error) {
	operation := operationFromInput(operationInput)
	state.OperationID = operation.ID
	state.HostID = operation.HostID
	if state.CreatedAt.IsZero() {
		state.CreatedAt = operation.CreatedAt
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = state.CreatedAt
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := insertOperation(tx, operation); err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO docker_host_agent_installations (
				operation_id, host_id, download_token_hash, download_expires_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`, state.OperationID, state.HostID, state.DownloadTokenHash, state.DownloadExpiresAt, state.CreatedAt, state.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create Docker host Agent installation: %w", err)
		}
		return nil
	})
	if err != nil {
		return domaindocker.Operation{}, err
	}
	return operation, nil
}

func (r *Repository) ConsumeHostAgentInstallTicket(ctx context.Context, operationID, downloadTokenHash, enrollmentTokenHash string, enrollmentExpiresAt, now time.Time) (domaindocker.HostAgentInstallationState, error) {
	var state domaindocker.HostAgentInstallationState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = lockHostAgentInstallation(tx, operationID)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(state.DownloadTokenHash), []byte(downloadTokenHash)) != 1 {
			return ErrNotFound
		}
		if state.DownloadedAt != nil || !now.Before(state.DownloadExpiresAt) {
			return domaindocker.ErrHostAgentInstallationExpired
		}
		result := tx.Exec(`
			UPDATE docker_host_agent_installations
			SET downloaded_at = ?, enrollment_token_hash = ?, enrollment_expires_at = ?, updated_at = ?
			WHERE operation_id = ? AND downloaded_at IS NULL
		`, now, enrollmentTokenHash, enrollmentExpiresAt, now, state.OperationID)
		if result.Error != nil {
			return fmt.Errorf("consume Docker host Agent install ticket: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return domaindocker.ErrHostAgentInstallationExpired
		}
		state.DownloadedAt = &now
		state.EnrollmentTokenHash = enrollmentTokenHash
		state.EnrollmentExpiresAt = &enrollmentExpiresAt
		state.UpdatedAt = now
		return nil
	})
	return state, err
}

func (r *Repository) ExchangeHostAgentEnrollment(ctx context.Context, input domaindocker.HostAgentEnrollmentExchange) (domaindocker.HostAgentInstallationState, error) {
	var state domaindocker.HostAgentInstallationState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = lockHostAgentInstallation(tx, input.OperationID)
		if err != nil {
			return err
		}
		if state.EnrolledAt != nil {
			return domaindocker.ErrHostAgentEnrollmentConsumed
		}
		if state.DownloadedAt == nil || subtle.ConstantTimeCompare([]byte(state.EnrollmentTokenHash), []byte(input.EnrollmentTokenHash)) != 1 {
			return ErrNotFound
		}
		if state.EnrollmentExpiresAt == nil || !input.EnrolledAt.Before(*state.EnrollmentExpiresAt) {
			return domaindocker.ErrHostAgentInstallationExpired
		}
		if err := tx.Exec(`
			UPDATE docker_host_agent_installations
			SET revoked_at = ?, updated_at = ?
			WHERE host_id = ? AND operation_id <> ? AND runtime_token_hash IS NOT NULL AND revoked_at IS NULL
		`, input.EnrolledAt, input.EnrolledAt, state.HostID, state.OperationID).Error; err != nil {
			return fmt.Errorf("revoke previous Docker host Agent credential: %w", err)
		}
		result := tx.Exec(`
			UPDATE docker_host_agent_installations
			SET enrolled_at = ?, agent_id = ?, agent_token_ciphertext = ?, runtime_token_hash = ?, updated_at = ?
			WHERE operation_id = ? AND enrolled_at IS NULL
		`, input.EnrolledAt, input.AgentID, input.AgentTokenCiphertext, input.RuntimeTokenHash, input.EnrolledAt, state.OperationID)
		if result.Error != nil {
			return fmt.Errorf("exchange Docker host Agent enrollment: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return domaindocker.ErrHostAgentEnrollmentConsumed
		}
		state.EnrolledAt = &input.EnrolledAt
		state.AgentID = input.AgentID
		state.AgentTokenCiphertext = input.AgentTokenCiphertext
		state.RuntimeTokenHash = input.RuntimeTokenHash
		state.UpdatedAt = input.EnrolledAt
		return nil
	})
	return state, err
}

func (r *Repository) GetHostAgentInstallationByRuntimeTokenHash(ctx context.Context, runtimeTokenHash string) (domaindocker.HostAgentInstallationState, error) {
	row := r.db.WithContext(ctx).Raw(hostAgentInstallationSelect()+`
		WHERE runtime_token_hash = ? AND enrolled_at IS NOT NULL AND revoked_at IS NULL
		LIMIT 1
	`, strings.TrimSpace(runtimeTokenHash)).Row()
	return scanHostAgentInstallationRow(row)
}

func (r *Repository) GetActiveHostAgentInstallation(ctx context.Context, hostID string) (domaindocker.HostAgentInstallationState, error) {
	row := r.db.WithContext(ctx).Raw(hostAgentInstallationSelect()+`
		WHERE host_id = ? AND enrolled_at IS NOT NULL AND revoked_at IS NULL
		LIMIT 1
	`, strings.TrimSpace(hostID)).Row()
	return scanHostAgentInstallationRow(row)
}

func lockHostAgentInstallation(tx *gorm.DB, operationID string) (domaindocker.HostAgentInstallationState, error) {
	row := tx.Raw(hostAgentInstallationSelect()+" WHERE operation_id = ? FOR UPDATE", strings.TrimSpace(operationID)).Row()
	return scanHostAgentInstallationRow(row)
}

func hostAgentInstallationSelect() string {
	return `SELECT operation_id, host_id, download_token_hash, download_expires_at, downloaded_at,
		COALESCE(enrollment_token_hash, ''), enrollment_expires_at, enrolled_at, COALESCE(agent_id, ''),
		COALESCE(agent_token_ciphertext, ''), COALESCE(runtime_token_hash, ''), revoked_at, created_at, updated_at
		FROM docker_host_agent_installations`
}

func scanHostAgentInstallationRow(row *sql.Row) (domaindocker.HostAgentInstallationState, error) {
	var state domaindocker.HostAgentInstallationState
	if err := row.Scan(
		&state.OperationID, &state.HostID, &state.DownloadTokenHash, &state.DownloadExpiresAt, &state.DownloadedAt,
		&state.EnrollmentTokenHash, &state.EnrollmentExpiresAt, &state.EnrolledAt, &state.AgentID,
		&state.AgentTokenCiphertext, &state.RuntimeTokenHash, &state.RevokedAt, &state.CreatedAt, &state.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindocker.HostAgentInstallationState{}, ErrNotFound
		}
		return domaindocker.HostAgentInstallationState{}, fmt.Errorf("scan Docker host Agent installation: %w", err)
	}
	if state.OperationID == "" || state.HostID == "" {
		return domaindocker.HostAgentInstallationState{}, apperrors.ErrNotFound
	}
	return state, nil
}
