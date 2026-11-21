package assistantmesh

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

const (
	delegationPageSize       = 100
	delegationOwnerOpenLimit = 20
	delegationPairHourLimit  = 5
)

const delegationColumns = `id, caller_id, owner_id, request_id, capability, capability_field, input_hash,
	status, result, result_hash, device_id, mode, ticket_hash, ticket_expires_at, fence, created_at, expires_at, closed_at`

func (r *Repository) PutManagedDevice(ctx context.Context, device domain.ManagedDevice) error {
	if err := validateManagedDevice(device); err != nil {
		return err
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := lockInquiryParticipants(ctx, db, device.OwnerID, device.OwnerID); err != nil {
			return err
		}
		open, err := lockInquiryOwner(ctx, db, device.OwnerID)
		if err != nil {
			return err
		}
		if !open {
			return fmt.Errorf("%w: owner is not accepting delegation", apperrors.ErrConflict)
		}
		if err := db.Exec(`
			INSERT INTO assistant_mesh_managed_devices (owner_id, device_id, session_id, proof_hash)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (owner_id) DO UPDATE SET
				device_id = EXCLUDED.device_id,
				session_id = EXCLUDED.session_id,
				proof_hash = EXCLUDED.proof_hash,
				updated_at = now()
		`, device.OwnerID, device.DeviceID, device.SessionID, device.ProofHash).Error; err != nil {
			return fmt.Errorf("put assistant mesh managed device: %w", err)
		}
		return nil
	})
}

func (r *Repository) GetManagedDevice(ctx context.Context, ownerID string) (domain.ManagedDevice, error) {
	if !canonicalUUID(ownerID) {
		return domain.ManagedDevice{}, fmt.Errorf("%w: managed device owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	device, err := getManagedDevice(ctx, dbtx.DB(ctx, r.db), ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ManagedDevice{}, apperrors.ErrNotFound
	}
	return device, err
}

func getManagedDevice(ctx context.Context, db *gorm.DB, ownerID string) (domain.ManagedDevice, error) {
	var device domain.ManagedDevice
	err := db.WithContext(ctx).Raw(`
		SELECT d.owner_id, d.device_id, d.session_id, d.proof_hash
		FROM assistant_mesh_managed_devices d
		JOIN assistant_mesh_profiles p ON p.owner_id = d.owner_id AND p.open
		JOIN users u ON u.id = d.owner_id AND u.status = 'active'
		JOIN ai_personal_agent_access a ON a.user_id = d.owner_id::text AND a.status = 'approved'
		WHERE d.owner_id = ?
	`, ownerID).Row().Scan(&device.OwnerID, &device.DeviceID, &device.SessionID, &device.ProofHash)
	if err != nil {
		return domain.ManagedDevice{}, err
	}
	return device, nil
}

func (r *Repository) CreateDelegation(ctx context.Context, value domain.DelegationRecord) (domain.DelegationRecord, error) {
	if err := validateDelegation(value, true); err != nil {
		return domain.DelegationRecord{}, err
	}
	var result domain.DelegationRecord
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		existing, found, err := delegationByRequest(ctx, db, value.CallerID, value.RequestID)
		if err != nil {
			return err
		}
		if found {
			result, err = delegationReceipt(existing, value)
			return err
		}
		if err := lockInquiryParticipants(ctx, db, value.CallerID, value.OwnerID); err != nil {
			return err
		}
		existing, found, err = delegationByRequest(ctx, db, value.CallerID, value.RequestID)
		if err != nil {
			return err
		}
		if found {
			result, err = delegationReceipt(existing, value)
			return err
		}
		open, err := lockInquiryOwner(ctx, db, value.OwnerID)
		if err != nil {
			return err
		}
		if !open {
			return fmt.Errorf("%w: owner is not accepting delegation", apperrors.ErrConflict)
		}
		device, err := getManagedDevice(ctx, db, value.OwnerID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: owner has no opted-in managed device", apperrors.ErrConflict)
		}
		if err != nil {
			return fmt.Errorf("get delegation managed device: %w", err)
		}
		if value.DeviceID != "" && value.DeviceID != device.DeviceID {
			return fmt.Errorf("%w: delegation device does not match the owner device", apperrors.ErrConflict)
		}
		value.DeviceID = device.DeviceID
		if err := checkDelegationCapacity(ctx, db, value); err != nil {
			return err
		}
		result, err = insertDelegation(ctx, db, value)
		return err
	})
	return result, err
}

func (r *Repository) GetDelegation(ctx context.Context, id string) (domain.DelegationRecord, error) {
	if !canonicalUUID(id) {
		return domain.DelegationRecord{}, fmt.Errorf("%w: delegation id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	item, err := scanDelegation(dbtx.DB(ctx, r.db).Raw(
		`SELECT `+delegationColumns+` FROM assistant_mesh_delegations WHERE id = ?`, id,
	).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DelegationRecord{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.DelegationRecord{}, fmt.Errorf("get assistant mesh delegation: %w", err)
	}
	return item, nil
}

func (r *Repository) ListDelegations(ctx context.Context, userID string, inbox bool) ([]domain.DelegationRecord, error) {
	if !canonicalUUID(userID) {
		return nil, fmt.Errorf("%w: delegation participant must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	column := "caller_id"
	if inbox {
		column = "owner_id"
	}
	rows, err := dbtx.DB(ctx, r.db).Raw(
		`SELECT `+delegationColumns+` FROM assistant_mesh_delegations WHERE `+column+` = ?
		 ORDER BY created_at DESC, id DESC LIMIT ?`, userID, delegationPageSize,
	).Rows()
	if err != nil {
		return nil, fmt.Errorf("list assistant mesh delegations: %w", err)
	}
	defer rows.Close()
	items := make([]domain.DelegationRecord, 0)
	for rows.Next() {
		item, err := scanDelegation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan assistant mesh delegation: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assistant mesh delegations: %w", err)
	}
	return items, nil
}

func (r *Repository) UpdateDelegation(ctx context.Context, id, ownerID string, apply func(context.Context, *domain.DelegationRecord) error) (domain.DelegationRecord, error) {
	if !canonicalUUID(id) {
		return domain.DelegationRecord{}, fmt.Errorf("%w: delegation id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if !canonicalUUID(ownerID) {
		return domain.DelegationRecord{}, fmt.Errorf("%w: delegation owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if apply == nil {
		return domain.DelegationRecord{}, fmt.Errorf("%w: delegation update is required", apperrors.ErrInvalidArgument)
	}
	var result domain.DelegationRecord
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		var callerID, selectedOwner string
		err := db.Raw(`SELECT caller_id, owner_id FROM assistant_mesh_delegations WHERE id = ?`, id).
			Row().Scan(&callerID, &selectedOwner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && selectedOwner != ownerID) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read assistant mesh delegation owner: %w", err)
		}
		if err := lockInquiryParticipants(ctx, db, callerID, ownerID); err != nil {
			return err
		}
		open, err := lockInquiryOwner(ctx, db, ownerID)
		if err != nil {
			return err
		}
		if !open {
			return fmt.Errorf("%w: owner is not accepting delegation", apperrors.ErrConflict)
		}
		item, err := scanDelegation(db.Raw(
			`SELECT `+delegationColumns+` FROM assistant_mesh_delegations WHERE id = ? AND owner_id = ? FOR UPDATE`,
			id, ownerID,
		).Row())
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock assistant mesh delegation: %w", err)
		}
		before := item
		if err := apply(ctx, &item); err != nil {
			return err
		}
		if err := sameFrozenDelegation(before, item); err != nil {
			return err
		}
		if err := validateDelegation(item, false); err != nil {
			return err
		}
		update := db.Exec(`
			UPDATE assistant_mesh_delegations SET
				status = ?, result = ?, result_hash = ?, ticket_hash = ?, ticket_expires_at = ?,
				fence = ?, device_id = ?, mode = ?, closed_at = ?
			WHERE id = ? AND caller_id = ? AND owner_id = ?
		`, item.Status, item.Result, nullableHash(item.ResultHash), nullableHash(item.TicketHash),
			nullableTime(item.TicketExpiresAt), item.Fence, nullableText(item.DeviceID), nullableText(item.Mode), item.ClosedAt,
			id, item.CallerID, item.OwnerID)
		if update.Error != nil {
			return fmt.Errorf("update assistant mesh delegation: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return apperrors.ErrNotFound
		}
		result = item
		return nil
	})
	return result, err
}

func (r *Repository) CleanupDelegations(ctx context.Context, now time.Time, retention time.Duration) error {
	if retention <= 0 {
		return fmt.Errorf("%w: delegation retention must be positive", apperrors.ErrInvalidArgument)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := db.Exec(`
			WITH due AS (
				SELECT id FROM assistant_mesh_delegations
				WHERE status IN ('awaiting_owner_approval', 'approved', 'executing', 'awaiting_confirmation')
				  AND expires_at <= ?
				ORDER BY expires_at LIMIT 200 FOR UPDATE SKIP LOCKED
			)
			UPDATE assistant_mesh_delegations d
			SET status = 'expired', closed_at = d.expires_at, ticket_hash = NULL,
				ticket_expires_at = NULL, result_hash = NULL
			FROM due WHERE d.id = due.id
		`, now).Error; err != nil {
			return fmt.Errorf("expire assistant mesh delegations: %w", err)
		}
		if err := db.Exec(`
			WITH ready AS (
				SELECT id FROM assistant_mesh_delegations
				WHERE (result <> '' OR result_hash IS NOT NULL) AND closed_at IS NOT NULL
				  AND closed_at + make_interval(secs => ?) <= ?
				ORDER BY closed_at LIMIT 200 FOR UPDATE SKIP LOCKED
			)
			UPDATE assistant_mesh_delegations d SET result = '', result_hash = NULL
			FROM ready WHERE d.id = ready.id
		`, int(retention.Seconds()), now).Error; err != nil {
			return fmt.Errorf("purge assistant mesh delegation results: %w", err)
		}
		return nil
	})
}

func checkDelegationCapacity(ctx context.Context, db *gorm.DB, item domain.DelegationRecord) error {
	var ownerOpen, pairHour int
	err := db.Raw(`
		SELECT
			(SELECT count(*) FROM assistant_mesh_delegations
			 WHERE owner_id = ? AND status IN ('awaiting_owner_approval', 'approved', 'executing', 'awaiting_confirmation')
			   AND expires_at > ?),
			(SELECT count(*) FROM assistant_mesh_delegations
			 WHERE caller_id = ? AND owner_id = ? AND created_at >= ?)
	`, item.OwnerID, item.CreatedAt, item.CallerID, item.OwnerID, item.CreatedAt.Add(-time.Hour)).
		Row().Scan(&ownerOpen, &pairHour)
	if err != nil {
		return fmt.Errorf("count delegation admission: %w", err)
	}
	if ownerOpen >= delegationOwnerOpenLimit {
		return fmt.Errorf("%w: owner has too many open delegations", apperrors.ErrConflict)
	}
	if pairHour >= delegationPairHourLimit {
		return fmt.Errorf("%w: delegation pair hourly limit reached", apperrors.ErrConflict)
	}
	return nil
}

func insertDelegation(ctx context.Context, db *gorm.DB, item domain.DelegationRecord) (domain.DelegationRecord, error) {
	stored, err := scanDelegation(db.Raw(`
		INSERT INTO assistant_mesh_delegations
			(id, caller_id, owner_id, request_id, capability, capability_field, input_hash, status, result,
			 result_hash, device_id, mode, ticket_hash, ticket_expires_at, fence, created_at, expires_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (caller_id, request_id) DO NOTHING
		RETURNING `+delegationColumns,
		item.ID, item.CallerID, item.OwnerID, item.RequestID, item.Capability, item.Field, item.InputHash,
		item.Status, item.Result, nullableHash(item.ResultHash), nullableText(item.DeviceID), nullableText(item.Mode),
		nullableHash(item.TicketHash), nullableTime(item.TicketExpiresAt), item.Fence,
		item.CreatedAt, item.ExpiresAt, item.ClosedAt,
	).Row())
	if !errors.Is(err, sql.ErrNoRows) {
		return stored, err
	}
	existing, found, err := delegationByRequest(ctx, db, item.CallerID, item.RequestID)
	if err != nil {
		return domain.DelegationRecord{}, err
	}
	if !found {
		return domain.DelegationRecord{}, fmt.Errorf("%w: delegation request was not stored", apperrors.ErrConflict)
	}
	return delegationReceipt(existing, item)
}

func delegationByRequest(ctx context.Context, db *gorm.DB, callerID, requestID string) (domain.DelegationRecord, bool, error) {
	item, err := scanDelegation(db.Raw(
		`SELECT `+delegationColumns+` FROM assistant_mesh_delegations WHERE caller_id = ? AND request_id = ?`,
		callerID, requestID,
	).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DelegationRecord{}, false, nil
	}
	if err != nil {
		return domain.DelegationRecord{}, false, fmt.Errorf("get assistant mesh delegation request: %w", err)
	}
	return item, true, nil
}

func delegationReceipt(existing, item domain.DelegationRecord) (domain.DelegationRecord, error) {
	if existing.OwnerID != item.OwnerID || existing.InputHash != item.InputHash {
		return domain.DelegationRecord{}, fmt.Errorf("%w: delegation request is bound to different input", apperrors.ErrConflict)
	}
	return existing, nil
}

func sameFrozenDelegation(before, after domain.DelegationRecord) error {
	if before.ID != after.ID || before.CallerID != after.CallerID || before.OwnerID != after.OwnerID ||
		before.RequestID != after.RequestID || before.Capability != after.Capability || before.Field != after.Field ||
		before.InputHash != after.InputHash || !before.CreatedAt.Equal(after.CreatedAt) ||
		!before.ExpiresAt.Equal(after.ExpiresAt) {
		return fmt.Errorf("%w: delegation identity, input, or deadline cannot change", apperrors.ErrConflict)
	}
	return nil
}

func validateManagedDevice(device domain.ManagedDevice) error {
	if !canonicalUUID(device.OwnerID) {
		return fmt.Errorf("%w: managed device owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if !validBoundedID(device.DeviceID, 200) || !validBoundedID(device.SessionID, 512) {
		return fmt.Errorf("%w: managed device id and session must be bounded text", apperrors.ErrInvalidArgument)
	}
	if !validSHA256(device.ProofHash) {
		return fmt.Errorf("%w: managed device proof must be a lowercase SHA256", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateFrozenDelegation(item domain.DelegationRecord) error {
	if !canonicalUUID(item.ID) || !canonicalUUID(item.CallerID) || !canonicalUUID(item.OwnerID) ||
		!canonicalUUID(item.RequestID) {
		return fmt.Errorf("%w: delegation id, participants, and request must be canonical UUIDs", apperrors.ErrInvalidArgument)
	}
	if item.CallerID == item.OwnerID {
		return fmt.Errorf("%w: delegation participants must differ", apperrors.ErrInvalidArgument)
	}
	if !validDelegationCapability(item.Capability) || !validDelegationField(item.Field) {
		return fmt.Errorf("%w: unsupported delegation capability or field", apperrors.ErrInvalidArgument)
	}
	if !validSHA256(item.InputHash) {
		return fmt.Errorf("%w: delegation input hash must be a lowercase SHA256", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateDelegation(item domain.DelegationRecord, creation bool) error {
	if err := validateFrozenDelegation(item); err != nil {
		return err
	}
	if item.ResultHash != "" && !validSHA256(item.ResultHash) {
		return fmt.Errorf("%w: delegation result hash must be a lowercase SHA256", apperrors.ErrInvalidArgument)
	}
	if item.TicketHash != "" && !validSHA256(item.TicketHash) {
		return fmt.Errorf("%w: delegation ticket digest must be a lowercase SHA256", apperrors.ErrInvalidArgument)
	}
	if item.DeviceID != "" && !validBoundedID(item.DeviceID, 200) {
		return fmt.Errorf("%w: delegation device id must be bounded text", apperrors.ErrInvalidArgument)
	}
	if item.Mode != "" && !validBoundedID(item.Mode, 32) {
		return fmt.Errorf("%w: delegation mode must be bounded text", apperrors.ErrInvalidArgument)
	}
	if item.Fence < 0 {
		return fmt.Errorf("%w: delegation fence must not be negative", apperrors.ErrInvalidArgument)
	}
	if !validDelegationStatus(item.Status) {
		return fmt.Errorf("%w: invalid delegation status", apperrors.ErrInvalidArgument)
	}
	if creation && (item.Status != domain.DelegationAwaitingApproval || item.ClosedAt != nil) {
		return fmt.Errorf("%w: new delegation must await owner approval", apperrors.ErrInvalidArgument)
	}
	if item.CreatedAt.IsZero() || item.ExpiresAt.IsZero() || !item.ExpiresAt.After(item.CreatedAt) {
		return fmt.Errorf("%w: delegation deadlines are invalid", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validDelegationCapability(value string) bool {
	return value == domain.ManagedOSVersion
}

func validDelegationField(value string) bool {
	return value == "productVersion" || value == "buildVersion"
}

func validDelegationStatus(value string) bool {
	switch value {
	case domain.DelegationAwaitingApproval, domain.DelegationApproved, domain.DelegationExecuting,
		domain.DelegationPreviewed, domain.DelegationCompleted, domain.DelegationDeclined,
		domain.DelegationInvalidated, domain.DelegationExpired, domain.DelegationFailed:
		return true
	default:
		return false
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validBoundedID(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func nullableHash(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type delegationScanner interface {
	Scan(dest ...any) error
}

func scanDelegation(scanner delegationScanner) (domain.DelegationRecord, error) {
	var item domain.DelegationRecord
	var resultHash, deviceID, mode, ticketHash sql.NullString
	var ticketExpiresAt, closedAt sql.NullTime
	if err := scanner.Scan(&item.ID, &item.CallerID, &item.OwnerID, &item.RequestID, &item.Capability,
		&item.Field, &item.InputHash, &item.Status, &item.Result, &resultHash, &deviceID, &mode,
		&ticketHash, &ticketExpiresAt, &item.Fence, &item.CreatedAt, &item.ExpiresAt, &closedAt); err != nil {
		return domain.DelegationRecord{}, err
	}
	item.ResultHash, item.DeviceID, item.Mode, item.TicketHash = resultHash.String, deviceID.String, mode.String, ticketHash.String
	if ticketExpiresAt.Valid {
		item.TicketExpiresAt = ticketExpiresAt.Time
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	return item, nil
}
