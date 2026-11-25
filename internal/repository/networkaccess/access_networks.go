package networkaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const accessNetworkColumns = `id, site_id, configuration, created_at, updated_at, deleted_at, activated`

type accessNetworkRecord struct {
	ID            string
	SiteID        string
	Configuration domain.AccessNetworkInput
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Deleted       bool
	Activated     bool
}

func (record accessNetworkRecord) network() domain.AccessNetwork {
	return domain.AccessNetwork{
		ID:            record.ID,
		Configuration: record.Configuration,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		Deleted:       record.Deleted,
		Activated:     record.Activated,
	}
}

func scanAccessNetwork(row scanner) (accessNetworkRecord, error) {
	var item accessNetworkRecord
	var raw []byte
	var deleted sql.NullTime
	if err := row.Scan(&item.ID, &item.SiteID, &raw, &item.CreatedAt, &item.UpdatedAt, &deleted, &item.Activated); err != nil {
		return item, err
	}
	if err := json.Unmarshal(raw, &item.Configuration); err != nil {
		return item, fmt.Errorf("decode access network configuration: %w", err)
	}
	item.Deleted = deleted.Valid
	return item, nil
}

func (r *Repository) ListAccessNetworks(ctx context.Context, siteID string) ([]domain.AccessNetwork, error) {
	records, err := queryMany(ctx, r.db, `SELECT `+accessNetworkColumns+` FROM network_access_networks
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ? ORDER BY id ASC`, []any{siteID}, scanAccessNetwork)
	if err != nil {
		return nil, err
	}
	items := make([]domain.AccessNetwork, len(records))
	for index, record := range records {
		items[index] = record.network()
	}
	return items, nil
}

func (r *Repository) GetAccessNetwork(ctx context.Context, id string) (domain.AccessNetwork, error) {
	record, err := scanOne(r.db.WithContext(ctx).Raw(`SELECT `+accessNetworkColumns+` FROM network_access_networks
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND deleted_at IS NULL LIMIT 1`, id).Row(), scanAccessNetwork, "access network")
	return record.network(), err
}

func (r *Repository) SaveAccessNetwork(ctx context.Context, item domain.AccessNetwork, create bool) (domain.AccessNetwork, error) {
	if strings.TrimSpace(item.ID) == "" {
		return domain.AccessNetwork{}, accessNetworkInvalid("access_network_id_required", "Access network ID is required.", "接入网络 ID 不能为空。")
	}
	if err := validateAccessNetworkShape(item.Configuration); err != nil {
		return domain.AccessNetwork{}, err
	}
	item.Configuration = normalizeAccessNetworkConfiguration(item.Configuration)
	raw, err := json.Marshal(item.Configuration)
	if err != nil {
		return domain.AccessNetwork{}, fmt.Errorf("encode access network configuration: %w", err)
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := lockAccessNetworkForSave(tx, item, create)
		if err != nil {
			return err
		}
		if err := validateAccessNetworkBindings(tx, item.Configuration); err != nil {
			return err
		}
		if item.Configuration.Enabled && item.Configuration.Kind == domain.AccessNetworkStrict {
			if err := requireStrictNetworkDrained(tx, item.ID, item.Configuration, now); err != nil {
				return err
			}
		}
		if item.Configuration.Enabled && item.Configuration.Kind != domain.AccessNetworkBootstrap {
			if err := ensureAccessNetworkSelectorsDoNotConflict(ctx, tx, item.ID, item.Configuration); err != nil {
				return err
			}
		}
		activate := item.Configuration.Enabled && item.Configuration.Kind != domain.AccessNetworkBootstrap
		if activate && !existing.Activated {
			if err := checkLegacyPasswordSessions(tx, item.Configuration.SiteID, item.UpdatedAt); err != nil {
				return err
			}
		}
		if create {
			return insertAccessNetwork(tx, item, raw, activate)
		}
		return mutationError(tx.Exec(`UPDATE network_access_networks SET configuration = ?::jsonb, updated_at = ?, activated = activated OR ?
			WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND site_id = ? AND deleted_at IS NULL`,
			string(raw), item.UpdatedAt, activate, item.ID, item.Configuration.SiteID), "access network")
	})
	if err != nil {
		return domain.AccessNetwork{}, normalizeDatabaseError(err)
	}
	return r.GetAccessNetwork(ctx, item.ID)
}

func lockAccessNetworkForSave(tx *gorm.DB, item domain.AccessNetwork, create bool) (accessNetworkRecord, error) {
	if err := lockAccessNetworkSite(tx, item.Configuration.SiteID); err != nil {
		return accessNetworkRecord{}, err
	}
	existing, found, err := lockAccessNetwork(tx, item.ID)
	if err != nil {
		return accessNetworkRecord{}, err
	}
	if create {
		if found {
			return accessNetworkRecord{}, accessNetworkConflict("access_network_exists", "An access network with this ID already exists.", "该接入网络 ID 已存在。")
		}
		return existing, nil
	}
	if !found {
		return accessNetworkRecord{}, fmt.Errorf("%w: access network not found", apperrors.ErrNotFound)
	}
	if existing.Deleted {
		return accessNetworkRecord{}, accessNetworkConflict("access_network_deleted", "A deleted access network cannot be revived.", "已删除的接入网络不能恢复。")
	}
	return existing, validateAccessNetworkCatalog(existing, item.Configuration)
}

func requireStrictNetworkDrained(tx *gorm.DB, networkID string, cfg domain.AccessNetworkInput, now time.Time) error {
	attachment := "(a.called_station_id !~ '^([[:xdigit:]]{2}[:-]){5}[[:xdigit:]]{2}:' OR substr(a.called_station_id,19)=?)"
	attachmentArgs := []any{cfg.SSID}
	if cfg.AccessMedium == domain.AccessMediumWired {
		attachment = "((? AND (a.nas_port_id='' OR a.nas_port_id IN ?)) OR (? AND (a.nas_port IS NULL OR a.nas_port BETWEEN ? AND ?)))"
		attachmentArgs = []any{len(cfg.PortIDs) > 0, cfg.PortIDs, cfg.PortStart != nil, cfg.PortStart, cfg.PortEnd}
	}
	args := append([]any{networkID, cfg.SiteID, cfg.NASBindingIDs, cfg.SiteID}, attachmentArgs...)
	args = append(args, now)
	var live bool
	err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM network_nas_authorizations a
		LEFT JOIN network_nas_session_evidence e USING (tenant_id, workspace_id, session_id)
		LEFT JOIN network_runtime_sessions s ON s.tenant_id=a.tenant_id AND s.workspace_id=a.workspace_id AND s.id=a.session_id
		WHERE a.tenant_id='default' AND a.workspace_id='default'
		  AND (a.access_network_id=? OR (a.access_network_id='' AND s.site_id=?) OR (EXISTS (
			SELECT 1 FROM network_access_nas_bindings b
			WHERE b.tenant_id=a.tenant_id AND b.workspace_id=a.workspace_id AND b.id IN ? AND b.site_id=?
			  AND (b.id=a.admission_binding_id OR (b.runtime_id=a.runtime_id AND b.nas_id=a.nas_id))
		  ) AND `+attachment+`))
		  AND a.decision='allow' AND a.strict_ui_epoch=''
		  AND (a.valid_until>? OR s.status NOT IN ('revoked','expired') OR (e.state='active' AND e.stopped_at IS NULL))
	)`, args...).Row().Scan(&live)
	if err != nil {
		return err
	}
	if live {
		return accessNetworkConflict("strict_network_requires_drained_sessions", "End the current sessions before enabling strict mode.", "启用严格模式前请先结束现有会话。")
	}
	return nil
}

func checkLegacyPasswordSessions(tx *gorm.DB, siteID string, now time.Time) error {
	var active bool
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM network_runtime_sessions
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ?
		  AND authentication_method = 'password-compatible' AND status NOT IN ('revoked', 'expired')
		  AND valid_until > ?)`, siteID, now).Row().Scan(&active); err != nil {
		return err
	}
	if active {
		return accessNetworkConflict("access_network_legacy_sessions_active", "Wait for active legacy password sessions to expire or disconnect them before enabling this network.", "请先等待仍活跃的旧密码准入会话到期或将其断开，再启用此网络。")
	}
	return nil
}

func insertAccessNetwork(tx *gorm.DB, item domain.AccessNetwork, raw []byte, activate bool) error {
	var count int
	if err := tx.Raw(`SELECT count(*) FROM network_access_networks
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ? AND deleted_at IS NULL`, item.Configuration.SiteID).Row().Scan(&count); err != nil {
		return err
	}
	if count >= 200 {
		return accessNetworkConflict("access_network_limit", "A site can contain at most 200 access networks.", "每个站点最多可配置 200 个接入网络。")
	}
	return tx.Exec(`INSERT INTO network_access_networks
		(id, tenant_id, workspace_id, site_id, configuration, created_at, updated_at, activated)
		VALUES (?, 'default', 'default', ?, ?::jsonb, ?, ?, ?)`,
		item.ID, item.Configuration.SiteID, string(raw), item.CreatedAt, item.UpdatedAt, activate).Error
}

func (r *Repository) DeleteAccessNetwork(ctx context.Context, id string, now time.Time) error {
	return normalizeDatabaseError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var siteID string
		err := tx.Raw(`SELECT site_id FROM network_access_networks
			WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND deleted_at IS NULL`, id).Row().Scan(&siteID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: access network not found", apperrors.ErrNotFound)
		}
		if err != nil {
			return err
		}
		if err := lockAccessNetworkSite(tx, siteID); err != nil {
			return err
		}
		return mutationError(tx.Exec(`UPDATE network_access_networks SET deleted_at = ?, updated_at = ?
			WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND site_id = ? AND deleted_at IS NULL`,
			now, now, id, siteID), "access network")
	}))
}

func validateAccessNetworkShape(cfg domain.AccessNetworkInput) error {
	if strings.TrimSpace(cfg.SiteID) == "" {
		return accessNetworkInvalid("access_network_site_required", "Access network site is required.", "接入网络必须归属站点。")
	}
	if cfg.AccessMedium != domain.AccessMediumWiFi && cfg.AccessMedium != domain.AccessMediumWired {
		return accessNetworkInvalid("access_network_medium_invalid", "Access network medium must be wifi or wired.", "接入网络类型必须为 Wi-Fi 或有线。")
	}
	switch cfg.Kind {
	case domain.AccessNetworkStandard, domain.AccessNetworkStrict, domain.AccessNetworkBootstrap:
	default:
		return accessNetworkInvalid("access_network_kind_invalid", "Access network kind is invalid.", "接入网络类型无效。")
	}
	if cfg.Enabled && cfg.Kind != domain.AccessNetworkBootstrap && len(cfg.NASBindingIDs) == 0 {
		return accessNetworkInvalid("access_network_binding_required", "An enabled managed access network requires a NAS binding.", "启用的受管接入网络必须绑定 NAS。")
	}
	return nil
}

func validateAccessNetworkCatalog(existing accessNetworkRecord, next domain.AccessNetworkInput) error {
	if existing.SiteID != next.SiteID {
		return accessNetworkConflict("access_network_site_immutable", "An access network cannot move to another site.", "接入网络不能切换到其他站点。")
	}
	if existing.Configuration.AccessMedium != next.AccessMedium {
		return accessNetworkConflict("access_network_medium_immutable", "An access network cannot change its access medium.", "接入网络不能切换接入类型。")
	}
	if existing.Configuration.Kind != next.Kind &&
		(existing.Configuration.Kind == domain.AccessNetworkBootstrap || next.Kind == domain.AccessNetworkBootstrap) {
		return accessNetworkConflict("access_network_guest_immutable", "Guest bootstrap access networks cannot change to or from managed kinds.", "开放访客引导网络不能与受管类型互相切换。")
	}
	return nil
}

func validateAccessNetworkBindings(tx *gorm.DB, cfg domain.AccessNetworkInput) error {
	for _, bindingID := range cfg.NASBindingIDs {
		bindingID = strings.TrimSpace(bindingID)
		if bindingID == "" {
			return accessNetworkInvalid("access_network_binding_invalid", "Access network NAS binding is invalid.", "接入网络 NAS 绑定无效。")
		}
		var siteID, medium string
		err := tx.Raw(`SELECT site_id, COALESCE(access_medium, '') FROM network_access_nas_bindings
			WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? FOR SHARE`, bindingID).Row().Scan(&siteID, &medium)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (siteID != cfg.SiteID || medium != "" && medium != cfg.AccessMedium)) {
			return accessNetworkInvalid("access_network_binding_invalid", "Access network NAS binding must exist in the same site and support the access medium.", "NAS 绑定必须属于同一站点并支持该接入方式。")
		}
		if err != nil {
			return err
		}
		if cfg.Enabled && cfg.Kind == domain.AccessNetworkStrict {
			binding, err := New(tx).GetNASBinding(tx.Statement.Context, bindingID)
			if err != nil {
				return err
			}
			if !binding.SupportsStrictOnline() {
				return accessNetworkInvalid("strict_nas_qualification_required", "Strict online admission requires qualified NAS hardware.", "启用严格在线准入前需要完成 NAS 实机验收。")
			}
		}
	}
	return nil
}

func ensureAccessNetworkSelectorsDoNotConflict(ctx context.Context, tx *gorm.DB, id string, cfg domain.AccessNetworkInput) error {
	records, err := queryMany(ctx, tx, `SELECT `+accessNetworkColumns+` FROM network_access_networks
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ? AND deleted_at IS NULL AND id <> ?`,
		[]any{cfg.SiteID, id}, scanAccessNetwork)
	if err != nil {
		return err
	}
	for _, record := range records {
		other := record.Configuration
		if !other.Enabled || other.Kind == domain.AccessNetworkBootstrap {
			continue
		}
		if accessNetworkSelectorsConflict(cfg, other) {
			return accessNetworkConflict("access_network_selector_conflict", "Another enabled access network can match the same NAS selector.", "同一 NAS 上已有选择器可能重叠的启用接入网络。")
		}
	}
	return nil
}

func accessNetworkSelectorsConflict(one, two domain.AccessNetworkInput) bool {
	if one.AccessMedium != two.AccessMedium || !accessNetworksShareNAS(one.NASBindingIDs, two.NASBindingIDs) {
		return false
	}
	switch one.AccessMedium {
	case domain.AccessMediumWiFi:
		if one.SSID == "" || one.SSID != two.SSID {
			return false
		}
		return accessNetworkBSSIDsOverlap(one.BSSIDs, two.BSSIDs)
	case domain.AccessMediumWired:
		return accessNetworkWiredSelectorsOverlap(one, two)
	default:
		return false
	}
}

func accessNetworksShareNAS(one, two []string) bool {
	ids := make(map[string]struct{}, len(one))
	for _, id := range one {
		ids[strings.TrimSpace(id)] = struct{}{}
	}
	for _, id := range two {
		if _, ok := ids[strings.TrimSpace(id)]; ok {
			return true
		}
	}
	return false
}

func accessNetworkBSSIDsOverlap(one, two []string) bool {
	oneSet := normalizedAccessNetworkBSSIDs(one)
	twoSet := normalizedAccessNetworkBSSIDs(two)
	if len(oneSet) == 0 || len(twoSet) == 0 {
		return true
	}
	for bssid := range oneSet {
		if _, ok := twoSet[bssid]; ok {
			return true
		}
	}
	return false
}

func normalizedAccessNetworkBSSIDs(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeAccessNetworkBSSID(value)
		if value == "" {
			continue
		}
		result[value] = struct{}{}
	}
	return result
}

func normalizeAccessNetworkConfiguration(cfg domain.AccessNetworkInput) domain.AccessNetworkInput {
	if len(cfg.BSSIDs) == 0 {
		return cfg
	}
	bssids := make([]string, len(cfg.BSSIDs))
	for index, value := range cfg.BSSIDs {
		bssids[index] = normalizeAccessNetworkBSSID(value)
	}
	cfg.BSSIDs = bssids
	return cfg
}

func normalizeAccessNetworkBSSID(value string) string {
	value = strings.TrimSpace(value)
	if mac, err := net.ParseMAC(value); err == nil && len(mac) == 6 {
		return mac.String()
	}
	return strings.ToLower(value)
}

func accessNetworkWiredSelectorsOverlap(one, two domain.AccessNetworkInput) bool {
	onePorts := accessNetworkStringSet(one.PortIDs)
	twoPorts := accessNetworkStringSet(two.PortIDs)
	for id := range onePorts {
		if _, ok := twoPorts[id]; ok {
			return true
		}
	}
	oneStart, oneEnd, oneRange := accessNetworkPortRange(one.PortStart, one.PortEnd)
	twoStart, twoEnd, twoRange := accessNetworkPortRange(two.PortStart, two.PortEnd)
	if oneRange && twoRange && oneStart <= twoEnd && twoStart <= oneEnd {
		return true
	}
	// A request can carry both a port ID and a numeric port. Without an
	// authoritative mapping, a string selector and a numeric selector cannot be
	// proven disjoint, so fail closed.
	return (len(onePorts) > 0 && twoRange) || (oneRange && len(twoPorts) > 0)
}

func accessNetworkStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func accessNetworkPortRange(start, end *int64) (int64, int64, bool) {
	if start == nil || end == nil || *start > *end {
		return 0, 0, false
	}
	return *start, *end, true
}

func lockAccessNetworkSite(tx *gorm.DB, siteID string) error {
	var locked string
	err := tx.Raw(`SELECT id FROM network_access_sites
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? FOR UPDATE`, siteID).Row().Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: network site not found", apperrors.ErrNotFound)
	}
	return err
}

func lockAccessNetwork(tx *gorm.DB, id string) (accessNetworkRecord, bool, error) {
	record, err := scanAccessNetwork(tx.Raw(`SELECT `+accessNetworkColumns+` FROM network_access_networks
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? FOR UPDATE`, id).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return accessNetworkRecord{}, false, nil
	}
	return record, err == nil, err
}

func accessNetworkInvalid(code, english, chinese string) error {
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, code, english, chinese)
}

func accessNetworkConflict(code, english, chinese string) error {
	return apperrors.NewBusiness(apperrors.ErrConflict, code, english, chinese)
}
