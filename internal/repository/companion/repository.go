package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureProfile(ctx context.Context, ownerID string, now time.Time) (domaincompanion.Profile, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return domaincompanion.Profile{}, fmt.Errorf("%w: companion owner is required", apperrors.ErrInvalidArgument)
	}
	if err := ensureProfile(ctx, r.db, ownerID, now); err != nil {
		return domaincompanion.Profile{}, err
	}
	return getProfile(ctx, r.db, ownerID, false)
}

func (r *Repository) ApplyInteraction(ctx context.Context, input domaincompanion.ApplyInteraction) (domaincompanion.InteractionReceipt, bool, error) {
	var receipt domaincompanion.InteractionReceipt
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureProfile(ctx, tx, input.OwnerID, input.Now); err != nil {
			return err
		}
		profile, err := getProfile(ctx, tx, input.OwnerID, true)
		if err != nil {
			return err
		}
		existing, existingHash, found, err := getReceipt(ctx, tx, input.OwnerID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existingHash != input.InputHash {
				return fmt.Errorf("%w: Idempotency-Key is already bound to different input", apperrors.ErrConflict)
			}
			if err := json.Unmarshal(existing, &receipt); err != nil {
				return fmt.Errorf("decode companion interaction receipt: %w", err)
			}
			return nil
		}
		if input.ClientRevision > 0 && input.ClientRevision != profile.Revision {
			return fmt.Errorf("%w: companion profile revision changed", apperrors.ErrConflict)
		}
		if input.Cooldown > 0 {
			lastInteractionAt, found, err := getInteractionState(ctx, tx, input.OwnerID, input.PluginID, input.InteractionID)
			if err != nil {
				return err
			}
			if found && input.Now.Before(lastInteractionAt.Add(input.Cooldown)) {
				return fmt.Errorf("%w: companion interaction is cooling down until %s", apperrors.ErrConflict, lastInteractionAt.Add(input.Cooldown).UTC().Format(time.RFC3339))
			}
		}

		profile.ActivePluginID = input.PluginID
		profile.ActiveVersion = input.Version
		profile.Xp += input.RewardXP
		profile.Affinity += input.RewardAffinity
		profile.Level = levelForXP(profile.Xp)
		newUnlocks := unlockedAtLevel(profile.UnlockedIDs, input.Unlocks, profile.Level)
		profile.UnlockedIDs = append(profile.UnlockedIDs, newUnlocks...)
		sort.Strings(profile.UnlockedIDs)
		profile.Revision++
		profile.LastInteractionAt = ptrTime(input.Now)
		profile.UpdatedAt = input.Now
		if err := updateProfile(ctx, tx, profile); err != nil {
			return err
		}
		if err := putInteractionState(ctx, tx, input.OwnerID, input.PluginID, input.InteractionID, input.Now); err != nil {
			return err
		}

		receipt = domaincompanion.InteractionReceipt{
			IdempotencyKey:  input.IdempotencyKey,
			Applied:         true,
			XpAwarded:       input.RewardXP,
			AffinityAwarded: input.RewardAffinity,
			UnlockedIDs:     newUnlocks,
			Profile:         profile,
		}
		if err := putReceipt(ctx, tx, input.OwnerID, input.IdempotencyKey, "interaction", input.InputHash, receipt, input.Now); err != nil {
			return err
		}
		created = true
		return nil
	})
	return receipt, created, err
}

func (r *Repository) ResetProfile(ctx context.Context, input domaincompanion.ResetProfile) (domaincompanion.Profile, bool, error) {
	var profile domaincompanion.Profile
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureProfile(ctx, tx, input.OwnerID, input.Now); err != nil {
			return err
		}
		locked, err := getProfile(ctx, tx, input.OwnerID, true)
		if err != nil {
			return err
		}
		existing, existingHash, found, err := getReceipt(ctx, tx, input.OwnerID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existingHash != input.InputHash {
				return fmt.Errorf("%w: Idempotency-Key is already bound to different input", apperrors.ErrConflict)
			}
			if err := json.Unmarshal(existing, &profile); err != nil {
				return fmt.Errorf("decode companion reset receipt: %w", err)
			}
			return nil
		}
		profile = locked
		profile.ActivePluginID = input.PluginID
		profile.ActiveVersion = input.Version
		profile.Level = 1
		profile.Xp = 0
		profile.Affinity = 0
		profile.UnlockedIDs = []string{}
		profile.LastInteractionAt = nil
		profile.Revision++
		profile.UpdatedAt = input.Now
		if err := updateProfile(ctx, tx, profile); err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Exec(`DELETE FROM companion_interaction_states WHERE owner_id = ?`, input.OwnerID).Error; err != nil {
			return err
		}
		if err := putReceipt(ctx, tx, input.OwnerID, input.IdempotencyKey, "reset", input.InputHash, profile, input.Now); err != nil {
			return err
		}
		created = true
		return nil
	})
	return profile, created, err
}

func (r *Repository) PutArtifact(ctx context.Context, artifact domaincompanion.Artifact) (domaincompanion.Artifact, error) {
	existing, err := getArtifact(ctx, r.db, artifact.PluginID, artifact.Version, false)
	if err == nil {
		if existing.SHA256 != artifact.SHA256 {
			return domaincompanion.Artifact{}, fmt.Errorf("%w: companion version is immutable", apperrors.ErrConflict)
		}
		return existing, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return domaincompanion.Artifact{}, err
	}
	assets, err := json.Marshal(artifact.Assets)
	if err != nil {
		return domaincompanion.Artifact{}, fmt.Errorf("encode companion assets: %w", err)
	}
	pack, err := json.Marshal(artifact.Pack)
	if err != nil {
		return domaincompanion.Artifact{}, fmt.Errorf("encode companion pack manifest: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO companion_artifacts (
			plugin_id, version, sha256, size_bytes, checksum_status, signature_status,
			provenance_status, storage_digest, assets, pack_manifest, active, installed_at, retired_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?)
	`, artifact.PluginID, artifact.Version, artifact.SHA256, artifact.SizeBytes, artifact.ChecksumStatus,
		artifact.SignatureStatus, artifact.ProvenanceStatus, artifact.StorageDigest, assets, pack,
		artifact.Active, artifact.InstalledAt, artifact.RetiredAt).Error; err != nil {
		return domaincompanion.Artifact{}, err
	}
	return getArtifact(ctx, r.db, artifact.PluginID, artifact.Version, false)
}

func (r *Repository) ListArtifacts(ctx context.Context, pluginID string) ([]domaincompanion.Artifact, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT plugin_id, version, sha256, size_bytes, checksum_status, signature_status,
		       provenance_status, storage_digest, assets, pack_manifest, active, installed_at, retired_at
		FROM companion_artifacts
		WHERE plugin_id = ? AND retired_at IS NULL
		ORDER BY installed_at DESC, version DESC
	`, pluginID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domaincompanion.Artifact{}
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ActivateArtifact(ctx context.Context, pluginID, version string, now time.Time) (domaincompanion.Artifact, error) {
	var active domaincompanion.Artifact
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := getArtifact(ctx, tx, pluginID, version, true)
		if err != nil {
			return err
		}
		if item.RetiredAt != nil {
			return fmt.Errorf("%w: companion artifact is retired", apperrors.ErrConflict)
		}
		if err := tx.WithContext(ctx).Exec(`UPDATE companion_artifacts SET active = false WHERE plugin_id = ?`, pluginID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Exec(`
			UPDATE companion_artifacts SET active = true, retired_at = NULL
			WHERE plugin_id = ? AND version = ?
		`, pluginID, version).Error; err != nil {
			return err
		}
		active = item
		active.Active = true
		_ = now
		return nil
	})
	return active, err
}

func (r *Repository) GetActiveArtifact(ctx context.Context, pluginID string) (domaincompanion.Artifact, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT plugin_id, version, sha256, size_bytes, checksum_status, signature_status,
		       provenance_status, storage_digest, assets, pack_manifest, active, installed_at, retired_at
		FROM companion_artifacts
		WHERE plugin_id = ? AND active = true AND retired_at IS NULL
		LIMIT 1
	`, pluginID).Row()
	item, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domaincompanion.Artifact{}, fmt.Errorf("%w: active companion artifact not found", apperrors.ErrNotFound)
	}
	return item, err
}

func (r *Repository) RetireArtifacts(ctx context.Context, pluginID string, now time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE companion_artifacts SET active = false, retired_at = ?
		WHERE plugin_id = ? AND retired_at IS NULL
	`, now, pluginID).Error
}

type rowScanner interface {
	Scan(...any) error
}

func ensureProfile(ctx context.Context, db *gorm.DB, ownerID string, now time.Time) error {
	return db.WithContext(ctx).Exec(`
		INSERT INTO companion_profiles (
			owner_id, id, active_plugin_id, active_version, level, xp, affinity,
			unlocked_ids, revision, created_at, updated_at
		) VALUES (?, ?, ?, ?, 1, 0, 0, '[]'::jsonb, 1, ?, ?)
		ON CONFLICT (owner_id) DO NOTHING
	`, ownerID, "companion:"+ownerID, domaincompanion.BuiltinPluginID, domaincompanion.BuiltinVersion, now, now).Error
}

func getProfile(ctx context.Context, db *gorm.DB, ownerID string, forUpdate bool) (domaincompanion.Profile, error) {
	query := `
		SELECT id, owner_id, active_plugin_id, active_version, level, xp, affinity,
		       unlocked_ids, revision, last_interaction_at, created_at, updated_at
		FROM companion_profiles WHERE owner_id = ?`
	if forUpdate {
		query += " FOR UPDATE"
	}
	row := db.WithContext(ctx).Raw(query, ownerID).Row()
	var profile domaincompanion.Profile
	var unlocked []byte
	if err := row.Scan(&profile.ID, &profile.OwnerID, &profile.ActivePluginID, &profile.ActiveVersion,
		&profile.Level, &profile.Xp, &profile.Affinity, &unlocked, &profile.Revision,
		&profile.LastInteractionAt, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincompanion.Profile{}, fmt.Errorf("%w: companion profile not found", apperrors.ErrNotFound)
		}
		return domaincompanion.Profile{}, err
	}
	if err := json.Unmarshal(unlocked, &profile.UnlockedIDs); err != nil {
		return domaincompanion.Profile{}, fmt.Errorf("decode companion unlocks: %w", err)
	}
	if profile.UnlockedIDs == nil {
		profile.UnlockedIDs = []string{}
	}
	return profile, nil
}

func updateProfile(ctx context.Context, db *gorm.DB, profile domaincompanion.Profile) error {
	unlocked, err := json.Marshal(profile.UnlockedIDs)
	if err != nil {
		return fmt.Errorf("encode companion unlocks: %w", err)
	}
	return db.WithContext(ctx).Exec(`
		UPDATE companion_profiles SET active_plugin_id = ?, active_version = ?, level = ?, xp = ?,
		       affinity = ?, unlocked_ids = ?::jsonb, revision = ?, last_interaction_at = ?, updated_at = ?
		WHERE owner_id = ?
	`, profile.ActivePluginID, profile.ActiveVersion, profile.Level, profile.Xp, profile.Affinity,
		unlocked, profile.Revision, profile.LastInteractionAt, profile.UpdatedAt, profile.OwnerID).Error
}

func getReceipt(ctx context.Context, db *gorm.DB, ownerID, key string) ([]byte, string, bool, error) {
	var response []byte
	var inputHash string
	err := db.WithContext(ctx).Raw(`
		SELECT response, input_hash FROM companion_idempotency_receipts
		WHERE owner_id = ? AND idempotency_key = ?
	`, ownerID, key).Row().Scan(&response, &inputHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	return response, inputHash, err == nil, err
}

func putReceipt(ctx context.Context, db *gorm.DB, ownerID, key, kind, inputHash string, response any, now time.Time) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode companion receipt: %w", err)
	}
	return db.WithContext(ctx).Exec(`
		INSERT INTO companion_idempotency_receipts (
			owner_id, idempotency_key, operation_kind, input_hash, response, created_at
		) VALUES (?, ?, ?, ?, ?::jsonb, ?)
	`, ownerID, key, kind, inputHash, raw, now).Error
}

func getInteractionState(ctx context.Context, db *gorm.DB, ownerID, pluginID, interactionID string) (time.Time, bool, error) {
	var lastInteractionAt time.Time
	err := db.WithContext(ctx).Raw(`
		SELECT last_interaction_at FROM companion_interaction_states
		WHERE owner_id = ? AND plugin_id = ? AND interaction_id = ?
		FOR UPDATE
	`, ownerID, pluginID, interactionID).Row().Scan(&lastInteractionAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return lastInteractionAt, err == nil, err
}

func putInteractionState(ctx context.Context, db *gorm.DB, ownerID, pluginID, interactionID string, now time.Time) error {
	return db.WithContext(ctx).Exec(`
		INSERT INTO companion_interaction_states (owner_id, plugin_id, interaction_id, last_interaction_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (owner_id, plugin_id, interaction_id)
		DO UPDATE SET last_interaction_at = EXCLUDED.last_interaction_at
	`, ownerID, pluginID, interactionID, now).Error
}

func getArtifact(ctx context.Context, db *gorm.DB, pluginID, version string, forUpdate bool) (domaincompanion.Artifact, error) {
	query := `
		SELECT plugin_id, version, sha256, size_bytes, checksum_status, signature_status,
		       provenance_status, storage_digest, assets, pack_manifest, active, installed_at, retired_at
		FROM companion_artifacts WHERE plugin_id = ? AND version = ?`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item, err := scanArtifact(db.WithContext(ctx).Raw(query, pluginID, version).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domaincompanion.Artifact{}, fmt.Errorf("%w: companion artifact not found", apperrors.ErrNotFound)
	}
	return item, err
}

func scanArtifact(row rowScanner) (domaincompanion.Artifact, error) {
	var item domaincompanion.Artifact
	var assets, pack []byte
	var retiredAt sql.NullTime
	if err := row.Scan(&item.PluginID, &item.Version, &item.SHA256, &item.SizeBytes,
		&item.ChecksumStatus, &item.SignatureStatus, &item.ProvenanceStatus, &item.StorageDigest,
		&assets, &pack, &item.Active, &item.InstalledAt, &retiredAt); err != nil {
		return domaincompanion.Artifact{}, err
	}
	if err := json.Unmarshal(assets, &item.Assets); err != nil {
		return domaincompanion.Artifact{}, fmt.Errorf("decode companion assets: %w", err)
	}
	if err := json.Unmarshal(pack, &item.Pack); err != nil {
		return domaincompanion.Artifact{}, fmt.Errorf("decode companion pack manifest: %w", err)
	}
	if retiredAt.Valid {
		item.RetiredAt = &retiredAt.Time
	}
	return item, nil
}

func levelForXP(xp int) int {
	level := 1 + xp/100
	if level > 100 {
		return 100
	}
	return level
}

func unlockedAtLevel(current []string, rules []domaincompanion.UnlockDefinition, level int) []string {
	result := []string{}
	for _, rule := range rules {
		if rule.Level <= level && !slices.Contains(current, rule.ID) && !slices.Contains(result, rule.ID) {
			result = append(result, rule.ID)
		}
	}
	sort.Strings(result)
	return result
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
