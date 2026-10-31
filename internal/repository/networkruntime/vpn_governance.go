package networkruntime

import (
	"errors"
	"slices"
	"strings"
	"time"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

type vpnGovernanceScope struct {
	RuntimeID                                           string
	SubjectID, DeviceID, SiteID, SpaceID, Mode, Profile string
	ResourceIDs                                         []string
}

func governanceRequired(site domain.Site, mode string, policies []domain.Policy) bool {
	return site.EffectiveDeviceGovernance() != domain.DeviceGovernanceLegacy || slices.ContainsFunc(policies, func(p domain.Policy) bool {
		return p.Enabled && len(p.DeviceManagementStates) != 0 && (len(p.SiteIDs) == 0 || slices.Contains(p.SiteIDs, site.ID)) && (len(p.Modes) == 0 || slices.Contains(p.Modes, mode))
	})
}

func vpnGovernanceAllowed(tx *gorm.DB, scope vpnGovernanceScope) (bool, error) {
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
	if strings.HasPrefix(scope.SubjectID, "service_account:") {
		return workloadScopeAllowed(tx, scope, time.Now().UTC())
	}
	if !governanceRequired(site, scope.Mode, snapshot.Policies) {
		return true, nil
	}
	device, err := repo.GetDevice(tx.Statement.Context, scope.DeviceID)
	if err != nil {
		return false, err
	}
	subject, err := repo.GetSubject(tx.Statement.Context, scope.SubjectID)
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrAccessDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if scope.Mode == domain.ModeExternalVPN {
		result := app.EvaluateAdmission(app.AdmissionInput{SubjectUserID: scope.SubjectID, DeviceID: scope.DeviceID, SiteID: scope.SiteID, Mode: scope.Mode}, subject, device, site, snapshot)
		return result.Decision == domain.DecisionAllow && result.NetworkProfile == scope.Profile, nil
	}
	return resourceVPNGovernanceAllowed(tx, scope, subject, device, site, snapshot)
}

func resourceVPNGovernanceAllowed(tx *gorm.DB, scope vpnGovernanceScope, subject domain.Subject, device domain.Device, site domain.Site, snapshot domain.PolicySnapshot) (bool, error) {
	repo := networkrepo.New(tx)
	ids := sortedUniqueStrings(scope.ResourceIDs)
	resources, err := repo.ResourcesByIDs(tx.Statement.Context, ids)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 || len(resources) != len(ids) {
		return false, nil
	}
	if scope.SpaceID == "" {
		scope.SpaceID = resources[0].SpaceID
	}
	space, err := repo.GetSpace(tx.Statement.Context, scope.SpaceID)
	if err != nil {
		return false, err
	}
	for _, resource := range resources {
		result := app.EvaluatePolicy(app.PreviewInput{SubjectUserID: scope.SubjectID, DeviceID: scope.DeviceID, SiteID: scope.SiteID, ResourceID: resource.ID, Mode: scope.Mode}, subject, device, site, space, resource, snapshot)
		if result.Decision != domain.DecisionAllow || result.NetworkProfile != scope.Profile {
			return false, nil
		}
	}
	return true, nil
}

func sessionGovernanceAllowed(tx *gorm.DB, session sessionRow, now time.Time) (bool, error) {
	networkLeases, leases, err := activeSessionLeases(tx, session.ID, now)
	if err != nil {
		return false, err
	}
	scope := vpnGovernanceScope{RuntimeID: session.RuntimeID, SubjectID: session.SubjectID, DeviceID: session.DeviceID, SiteID: session.SiteID, Mode: session.Mode, Profile: session.AccessProfile}
	for _, lease := range leases {
		scope.ResourceIDs = append(scope.ResourceIDs, lease.ResourceIDs...)
	}
	if len(networkLeases) > 0 {
		scope.SpaceID = networkLeases[0].NetworkSpaceID
	}

	return vpnGovernanceAllowed(tx, scope)
}

func revokeGovernanceSessions(tx *gorm.DB, runtimeID string, now time.Time) error {
	snapshot, err := networkrepo.New(tx).GetPolicySnapshot(tx.Statement.Context)
	if err != nil {
		return err
	}
	hasSelectors := slices.ContainsFunc(snapshot.Policies, func(p domain.Policy) bool { return p.Enabled && len(p.DeviceManagementStates) != 0 })
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
		allowed, err := sessionGovernanceAllowed(tx, session, now)
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

func requireVPNGovernance(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) error {
	allowed, err := vpnGovernanceAllowed(tx, vpnGovernanceScope{RuntimeID: connection.RuntimeID, SubjectID: connection.SubjectID, DeviceID: connection.DeviceID, SiteID: connection.SiteID, SpaceID: connection.NetworkSpaceID, Mode: connection.Mode, Profile: connection.AccessProfile, ResourceIDs: connection.ResourceIDs})
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.NewBusiness(apperrors.ErrConflict, "device_governance_changed", "Device governance changed before the VPN session was saved.", "VPN 会话保存前设备治理条件已变化。")
	}
	return nil
}

func validateReplayedVPNGovernance(tx *gorm.DB, sessionID string, now time.Time) error {
	var session sessionRow
	if err := tx.Where("tenant_id='default' AND workspace_id='default' AND id=?", sessionID).First(&session).Error; err != nil {
		return err
	}
	allowed, err := sessionGovernanceAllowed(tx, session, now)
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "device_governance_revoked", "Device governance no longer allows this VPN session.", "设备治理已不允许该 VPN 会话。")
	}
	return nil
}
