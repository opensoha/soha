package contenttransfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }
type transactionKey struct{}

func New(db *gorm.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("content transfer database is required")
	}
	return &Repository{db: db}, nil
}
func (r *Repository) dbFor(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(transactionKey{}).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return dbtx.DB(ctx, r.db)
}
func scanDestination(row *sql.Row) (domain.Destination, error) {
	var d domain.Destination
	var draft, published []byte
	err := row.Scan(&d.ID, &d.OwnerID, &d.Revision, &draft, &published, &d.PublishedVersion, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, apperrors.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(draft, &d.Draft); err != nil {
		return d, err
	}
	if len(published) > 0 && string(published) != "null" {
		d.Published = &domain.Config{}
		err = json.Unmarshal(published, d.Published)
	}
	return d, err
}
func (r *Repository) Destination(ctx context.Context, id string) (domain.Destination, error) {
	return scanDestination(r.dbFor(ctx).Raw(`SELECT id,owner_id,revision,draft,published,published_version,updated_at FROM content_transfer_destinations WHERE id=?`, id).Row())
}
func (r *Repository) Destinations(ctx context.Context) ([]domain.Destination, error) {
	rows, err := r.dbFor(ctx).Raw(`SELECT id FROM content_transfer_destinations ORDER BY id LIMIT 101`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 100 {
		return nil, apperrors.ErrServiceUnavailable
	}
	result := make([]domain.Destination, 0, len(ids))
	for _, id := range ids {
		d, err := r.Destination(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, nil
}
func audit(tx *gorm.DB, actor, destination, record, action string, metadata any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO content_transfer_audit(actor_id,destination_id,record_id,action,metadata) VALUES(?,?,?,?,?::jsonb)`, actor, destination, record, action, string(raw)).Error
}
func lock(tx *gorm.DB, id string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "managed-content/"+id).Error
}
func (r *Repository) Save(ctx context.Context, id, actor string, input domain.SaveInput) (domain.Destination, error) {
	var out domain.Destination
	raw, _ := json.Marshal(input.Draft)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx, "catalog"); err != nil {
			return err
		}
		if err := lock(tx, id); err != nil {
			return err
		}
		count := int64(0)
		if err := tx.Raw(`SELECT count(*) FROM content_transfer_destinations`).Row().Scan(&count); err != nil {
			return err
		}
		current, err := (&Repository{db: tx}).Destination(ctx, id)
		if errors.Is(err, apperrors.ErrNotFound) {
			if input.ExpectedRevision != 0 || count >= 100 {
				return apperrors.ErrConflict
			}
			q := tx.Exec(`INSERT INTO content_transfer_destinations(id,owner_id,revision,draft,updated_at) VALUES(?,?,1,?::jsonb,clock_timestamp())`, id, actor, string(raw))
			if q.Error != nil {
				return q.Error
			}
		} else {
			if err != nil {
				return err
			}
			if current.Revision != input.ExpectedRevision {
				return apperrors.ErrConflict
			}
			if err := tx.Exec(`UPDATE content_transfer_destinations SET revision=revision+1,draft=?::jsonb,updated_at=clock_timestamp() WHERE id=?`, string(raw), id).Error; err != nil {
				return err
			}
		}
		out, err = (&Repository{db: tx}).Destination(ctx, id)
		if err != nil {
			return err
		}
		return audit(tx, actor, id, "", "destination_saved", map[string]any{"revision": out.Revision})
	})
	return out, err
}
func (r *Repository) Publish(ctx context.Context, id, actor string, revision int64) (domain.Destination, error) {
	var out domain.Destination
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx, id); err != nil {
			return err
		}
		q := tx.Exec(`UPDATE content_transfer_destinations SET published=draft,revision=revision+1,published_version=revision+1,updated_at=clock_timestamp() WHERE id=? AND revision=?`, id, revision)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		var err error
		out, err = (&Repository{db: tx}).Destination(ctx, id)
		if err != nil {
			return err
		}
		return audit(tx, actor, id, "", "destination_published", map[string]any{"revision": out.PublishedVersion})
	})
	return out, err
}

// Publication locks remain held until the bounded effect and final bookkeeping
// complete. The separately committed intent survives a rollback after an effect.
func (r *Repository) WithCurrent(ctx context.Context, gates []domain.Gate, fn func(context.Context) error) error {
	return r.dbFor(ctx).Transaction(func(tx *gorm.DB) error {
		gates = slices.Clone(gates)
		slices.SortFunc(gates, func(a, b domain.Gate) int {
			if a.DestinationID < b.DestinationID {
				return -1
			}
			if a.DestinationID > b.DestinationID {
				return 1
			}
			return 0
		})
		for _, g := range gates {
			if err := lock(tx, g.DestinationID); err != nil {
				return err
			}
			var version int64
			if err := tx.Raw(`SELECT published_version FROM content_transfer_destinations WHERE id=? FOR SHARE`, g.DestinationID).Row().Scan(&version); err != nil {
				return err
			}
			if version != g.PolicyVersion {
				return apperrors.ErrConflict
			}
		}
		return fn(context.WithValue(ctx, transactionKey{}, tx))
	})
}
func scanRecord(row *sql.Row) (domain.Record, error) {
	var r domain.Record
	var metadata, private []byte
	var revision int64
	var state string
	var deleted sql.NullTime
	err := row.Scan(&metadata, &private, &revision, &state, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return r, apperrors.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(metadata, &r); err != nil {
		return r, err
	}
	if err = json.Unmarshal(private, &r.Private); err != nil {
		return r, err
	}
	r.Revision = revision
	r.State = state
	if deleted.Valid {
		r.DeletedAt = &deleted.Time
	}
	return r, nil
}
func (r *Repository) Record(ctx context.Context, id string) (domain.Record, error) {
	return scanRecord(r.dbFor(ctx).Raw(`SELECT metadata,private_metadata,revision,state,deleted_at FROM content_transfer_records WHERE id=?`, id).Row())
}
func recordAudit(tx *gorm.DB, r domain.Record, actor, action string) error {
	return audit(tx, actor, r.DestinationID, r.ID, action, map[string]any{"state": r.State, "action": r.Action, "revision": r.Revision, "gates": r.Gates, "decision": r.Decision})
}
func (r *Repository) Create(ctx context.Context, record domain.Record) error {
	metadata, _ := json.Marshal(record)
	private, _ := json.Marshal(record.Private)
	return r.dbFor(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Exec(`INSERT INTO content_transfer_records(id,actor_id,destination_id,source_id,action,state,revision,metadata,private_metadata,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?::jsonb,?::jsonb,?,?) ON CONFLICT(id) DO NOTHING`, record.ID, record.ActorID, record.DestinationID, record.SourceID, record.Action, record.State, string(metadata), string(private), record.CreatedAt, record.UpdatedAt)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return recordAudit(tx, record, record.ActorID, "decision_recorded")
	})
}
func (r *Repository) Update(ctx context.Context, record domain.Record, expected int64, actor, action string) error {
	record.Revision = expected + 1
	record.UpdatedAt = time.Now().UTC()
	metadata, _ := json.Marshal(record)
	private, _ := json.Marshal(record.Private)
	tx := r.dbFor(ctx)
	return tx.Transaction(func(tx *gorm.DB) error {
		q := tx.Exec(`UPDATE content_transfer_records SET metadata=?::jsonb,private_metadata=?::jsonb,revision=revision+1,state=?,updated_at=?,deleted_at=? WHERE id=? AND actor_id=? AND revision=?`, string(metadata), string(private), record.State, record.UpdatedAt, record.DeletedAt, record.ID, record.ActorID, expected)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		if record.Action == "delete" && record.State == "completed" {
			q = tx.Exec(`UPDATE content_transfer_records SET deleted_at=?,revision=revision+1 WHERE id=? AND actor_id=? AND state='completed' AND deleted_at IS NULL AND action IN ('upload','paste','copy')`, record.UpdatedAt, record.SourceID, record.ActorID)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return apperrors.ErrConflict
			}
		}
		return recordAudit(tx, record, actor, action)
	})
}

// Consume commits independently before external submission. A failed/uncertain
// effect must never roll back a single-use approval and make it replayable.
func (r *Repository) Consume(ctx context.Context, id, actor, hash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Exec(`UPDATE content_transfer_records SET private_metadata=jsonb_set(private_metadata,'{tokenConsumed}','true'::jsonb),revision=revision+1,updated_at=clock_timestamp() WHERE id=? AND actor_id=? AND private_metadata->>'tokenHash'=? AND private_metadata->>'tokenConsumed'='false' AND (private_metadata->>'tokenExpiresAt')::timestamptz>clock_timestamp() AND (private_metadata->>'expiresAt')::timestamptz>clock_timestamp()`, id, actor, hash)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return audit(tx, actor, "", id, "authorization_consumed", map[string]any{})
	})
}
func (r *Repository) Records(ctx context.Context, actor, destination, kind string, offset, limit int) (domain.Page, error) {
	where := " WHERE actor_id=?"
	args := []any{actor}
	if kind == "cases" {
		where = " WHERE destination_id=?"
		args = []any{destination}
	}
	if kind == "files" {
		where += " AND action IN ('upload','paste','copy') AND state='completed' AND deleted_at IS NULL"
	}
	p := domain.Page{Items: []domain.Record{}, Offset: offset, Limit: limit}
	if err := r.dbFor(ctx).Raw(`SELECT count(*) FROM content_transfer_records`+where, args...).Row().Scan(&p.Total); err != nil {
		return p, err
	}
	queryArgs := append(slices.Clone(args), limit, offset)
	rows, err := r.dbFor(ctx).Raw(`SELECT id FROM content_transfer_records`+where+` ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, queryArgs...).Rows()
	if err != nil {
		return p, err
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return p, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	for _, id := range ids {
		item, err := r.Record(ctx, id)
		if err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	return p, nil
}
