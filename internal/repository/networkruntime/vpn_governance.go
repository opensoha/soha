package networkruntime

import (
	"errors"
	"time"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

// The WireGuard lock is acquired first by each caller. Hold publication
// stable from the initial version/read check through the transaction commit.
func lockVPNGovernancePolicy(tx *gorm.DB) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtext('network-access-policy-snapshot:default:default'))`).Error
}

func vpnPolicyVersionCurrent(tx *gorm.DB, expected int) (bool, error) {
	var current int
	err := tx.Raw(`SELECT COALESCE(MAX(policy_version), 0) FROM network_access_policy_snapshots WHERE tenant_id='default' AND workspace_id='default'`).Scan(&current).Error
	return current == expected, err
}

func (r *Repository) vpnGovernanceAllowed(tx *gorm.DB, scope app.VPNGovernanceScope) (bool, error) {
	// Share locks keep administrative reclassification from racing this commit.
	for _, target := range []struct{ table, id string }{{"network_access_devices", scope.DeviceID}, {"network_access_sites", scope.SiteID}} {
		var id string
		if err := tx.Raw("SELECT id FROM "+target.table+" WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? FOR SHARE", target.id).Row().Scan(&id); err != nil {
			return false, err
		}
	}
	repo := networkrepo.New(tx)
	site, err := repo.GetSite(tx.Statement.Context, scope.SiteID)
	if err != nil {
		return false, err
	}
	snapshot, err := repo.GetPolicySnapshot(tx.Statement.Context)
	if err != nil {
		return false, err
	}
	facts := app.VPNGovernanceFacts{Scope: scope, Site: site, Snapshot: snapshot, EvaluatedAt: time.Now().UTC()}
	path := r.governance.Path(scope, site, snapshot)
	switch path {
	case app.VPNGovernanceLegacy:
		return r.governance.Allows(path, facts), nil
	case app.VPNGovernanceWorkload:
		return r.workloadVPNGovernanceAllowed(tx, facts)
	}
	return r.userVPNGovernanceAllowed(tx, path, facts)
}

func (r *Repository) userVPNGovernanceAllowed(tx *gorm.DB, path app.VPNGovernancePath, facts app.VPNGovernanceFacts) (bool, error) {
	repo := networkrepo.New(tx)
	var err error
	facts.Device, err = repo.GetDevice(tx.Statement.Context, facts.Scope.DeviceID)
	if err != nil {
		return false, err
	}
	facts.Subject, err = repo.GetSubject(tx.Statement.Context, facts.Scope.SubjectID)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrAccessDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if path == app.VPNGovernanceAdmission {
		return r.governance.Allows(path, facts), nil
	}
	return r.resourceVPNGovernanceAllowed(tx, path, facts)
}

func (r *Repository) resourceVPNGovernanceAllowed(tx *gorm.DB, path app.VPNGovernancePath, facts app.VPNGovernanceFacts) (bool, error) {
	repo := networkrepo.New(tx)
	ids := sortedUniqueStrings(facts.Scope.ResourceIDs)
	resources, err := repo.ResourcesByIDs(tx.Statement.Context, ids)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 || len(resources) != len(ids) {
		return false, nil
	}
	spaceID := facts.Scope.SpaceID
	if spaceID == "" {
		spaceID = resources[0].SpaceID
	}
	facts.Space, err = repo.GetSpace(tx.Statement.Context, spaceID)
	if err != nil {
		return false, err
	}
	for _, resource := range resources {
		facts.Resource = resource
		if !r.governance.Allows(path, facts) {
			return false, nil
		}
	}
	return true, nil
}

func (r *Repository) sessionGovernanceAllowed(tx *gorm.DB, session sessionRow, now time.Time) (bool, error) {
	networkLeases, leases, err := activeSessionLeases(tx, session.ID, now)
	if err != nil {
		return false, err
	}
	scope := app.VPNGovernanceScope{RuntimeID: session.RuntimeID, SubjectID: session.SubjectID, DeviceID: session.DeviceID, SiteID: session.SiteID, Mode: session.Mode, Profile: session.AccessProfile}
	for _, lease := range leases {
		scope.ResourceIDs = append(scope.ResourceIDs, lease.ResourceIDs...)
	}
	if len(networkLeases) > 0 {
		scope.SpaceID = networkLeases[0].NetworkSpaceID
	}

	return r.vpnGovernanceAllowed(tx, scope)
}

func (r *Repository) revokeGovernanceSessions(tx *gorm.DB, runtimeID string, now time.Time) error {
	snapshot, err := networkrepo.New(tx).GetPolicySnapshot(tx.Statement.Context)
	if err != nil {
		return err
	}
	hasSelectors := r.governance.HasManagementSelectors(snapshot)
	var sessions []sessionRow
	err = tx.Raw(`SELECT s.* FROM network_runtime_sessions s JOIN network_access_sites ns ON ns.id=s.site_id AND ns.tenant_id=s.tenant_id AND ns.workspace_id=s.workspace_id WHERE s.tenant_id='default' AND s.workspace_id='default'
 AND s.status IN ('pending','active','restricted','quarantine') AND (ns.device_governance <> 'legacy' OR ? OR s.subject_id LIKE 'service_account:%') AND EXISTS (
 SELECT 1 FROM network_wireguard_peers p JOIN network_access_gateways g ON g.id=p.gateway_id
 WHERE p.tenant_id=s.tenant_id AND p.workspace_id=s.workspace_id AND p.session_id=s.id AND p.status='active'
 AND (s.runtime_id=? OR g.runtime_id=?)) ORDER BY s.id LIMIT 1025`, hasSelectors, runtimeID, runtimeID).Scan(&sessions).Error
	if err != nil {
		return err
	}
	if len(sessions) > 1024 {
		return apperrors.ErrServiceUnavailable
	}
	for _, session := range sessions {
		allowed, err := r.sessionGovernanceAllowed(tx, session, now)
		if err != nil {
			return err
		}
		if !allowed {
			if err := revokeRuntimePeers(tx, session.RuntimeID, now, "device_governance_revoked"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) requireVPNGovernance(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) error {
	allowed, err := r.vpnGovernanceAllowed(tx, app.VPNGovernanceScope{RuntimeID: connection.RuntimeID, SubjectID: connection.SubjectID, DeviceID: connection.DeviceID, SiteID: connection.SiteID, SpaceID: connection.NetworkSpaceID, Mode: connection.Mode, Profile: connection.AccessProfile, ResourceIDs: connection.ResourceIDs})
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.NewBusiness(apperrors.ErrConflict, "device_governance_changed", "Device governance changed before the VPN session was saved.", "VPN 会话保存前设备治理条件已变化。")
	}
	return nil
}

func (r *Repository) validateReplayedVPNGovernance(tx *gorm.DB, sessionID string, now time.Time) error {
	var session sessionRow
	if err := tx.Where("tenant_id='default' AND workspace_id='default' AND id=?", sessionID).First(&session).Error; err != nil {
		return err
	}
	allowed, err := r.sessionGovernanceAllowed(tx, session, now)
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "device_governance_revoked", "Device governance no longer allows this VPN session.", "设备治理已不允许该 VPN 会话。")
	}
	return nil
}
