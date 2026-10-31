package dockerterminal

import (
	"context"
	"errors"
	"fmt"
	"time"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db   *gorm.DB
	keys keyring.Ring
}

func New(db *gorm.DB, keys keyring.Ring) *Repository { return &Repository{db: db, keys: keys} }

type leaseRow struct {
	ID, ActorID, CredentialHash, TicketHash, Status string
	CreatedAt, TicketExpiresAt, DeadlineAt          time.Time
	ClosedAt                                        *time.Time
	EncryptedPayload                                string
}

func (leaseRow) TableName() string { return "docker_native_terminals" }

func (repository *Repository) Create(ctx context.Context, record domaindocker.NativeTerminalRecord) error {
	row, err := repository.encode(record)
	if err != nil {
		return err
	}
	if row.Status != "pending" || row.TicketHash == "" || !row.TicketExpiresAt.After(time.Now()) || row.DeadlineAt.Before(row.TicketExpiresAt) {
		return apperrors.ErrInvalidArgument
	}
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(1150141)).Error; err != nil {
			return err
		}
		if err := terminalCapacity(tx, row.ActorID); err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
}

func terminalCapacity(tx *gorm.DB, actorID string) error {
	var counts struct{ Total, Actor int64 }
	if err := tx.Raw(`SELECT count(*) AS total, count(*) FILTER (WHERE actor_id = ?) AS actor
		FROM docker_native_terminals WHERE deadline_at > now() AND
		(status IN ('active', 'closing') OR (status = 'pending' AND ticket_expires_at > now()))`, actorID).Scan(&counts).Error; err != nil {
		return err
	}
	if counts.Total >= 64 || counts.Actor >= 4 {
		return fmt.Errorf("%w: native terminal capacity reached", apperrors.ErrServiceUnavailable)
	}
	return nil
}

func (repository *Repository) Get(ctx context.Context, actorID, id string) (domaindocker.NativeTerminalRecord, error) {
	var row leaseRow
	if err := repository.db.WithContext(ctx).Where("id = ? AND actor_id = ?", id, actorID).Take(&row).Error; err != nil {
		return domaindocker.NativeTerminalRecord{}, terminalReadError(err)
	}
	return repository.decode(row)
}

func terminalReadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return err
}

// Consume atomically moves pending to active. Ticket erasure prevents a second
// connection even when another Core replica receives the same original header.
func (repository *Repository) Consume(ctx context.Context, actorID, id, credentialHash, ticketHash string) (domaindocker.NativeTerminalRecord, error) {
	var row leaseRow
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&row).Clauses(clause.Returning{}).
			Where("id = ? AND actor_id = ? AND credential_hash = ? AND ticket_hash = ? AND ticket_hash <> '' AND status = 'pending' AND ticket_expires_at > now() AND deadline_at > now()", id, actorID, credentialHash, ticketHash).
			Updates(map[string]any{"status": "active", "ticket_hash": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		// Decode within the transaction: corrupt/mismatched protected state
		// must roll back consumption rather than leave an active phantom lease.
		_, err := repository.decode(row)
		return err
	})
	if err != nil {
		return domaindocker.NativeTerminalRecord{}, err
	}
	return repository.decode(row)
}

func (repository *Repository) RequestClose(ctx context.Context, actorID, id string) (domaindocker.NativeTerminalRecord, error) {
	if err := repository.db.WithContext(ctx).Exec(`UPDATE docker_native_terminals
		SET status = CASE WHEN status = 'pending' THEN 'closed' ELSE 'closing' END,
		closed_at = CASE WHEN status = 'pending' THEN now() ELSE closed_at END, ticket_hash = ''
		WHERE id = ? AND actor_id = ? AND status IN ('pending', 'active')`, id, actorID).Error; err != nil {
		return domaindocker.NativeTerminalRecord{}, err
	}
	return repository.Get(ctx, actorID, id)
}

func (repository *Repository) Finish(ctx context.Context, record domaindocker.NativeTerminalRecord) error {
	if record.Lease.Status != "closed" || record.Lease.ClosedAt == nil || record.Lease.Result == nil || record.Lease.Result.Validate() != nil {
		return apperrors.ErrInvalidArgument
	}
	row, err := repository.encode(record)
	if err != nil {
		return err
	}
	result := repository.db.WithContext(ctx).Model(&leaseRow{}).
		Where("id = ? AND actor_id = ? AND credential_hash = ? AND status IN ('active', 'closing')", row.ID, row.ActorID, row.CredentialHash).
		Updates(map[string]any{"status": "closed", "closed_at": row.ClosedAt, "ticket_hash": "", "encrypted_payload": row.EncryptedPayload})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func (repository *Repository) Cleanup(ctx context.Context) error {
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE docker_native_terminals SET status = 'expired', closed_at = now(), ticket_hash = ''
			WHERE id IN (SELECT id FROM docker_native_terminals WHERE
			(status IN ('active', 'closing') AND deadline_at <= now()) OR
			(status = 'pending' AND (ticket_expires_at <= now() OR deadline_at <= now()))
			ORDER BY deadline_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM docker_native_terminals WHERE id IN
			(SELECT id FROM docker_native_terminals WHERE status IN ('closed', 'expired')
			AND closed_at < now() - interval '24 hours' ORDER BY closed_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`).Error
	})
}
