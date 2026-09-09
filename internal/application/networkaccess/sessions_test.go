package networkaccess

import (
	"testing"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestBuildSessionActionPlanFallsBackToDisconnect(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	session := domainnetworkaccess.Session{
		ID: "session-1", RuntimeID: "freeradius-hq", SubjectID: "user-1", DeviceID: "device-1", SiteID: "site-hq", NASID: "nas-hq",
		Mode: domainnetworkaccess.ModeInternalDirect, Path: domainnetworkaccess.PathSiteDirect, AccessProfile: domainnetworkaccess.ProfileFull,
		Status: "active", PolicyVersion: 7, StartedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), UpdatedAt: now.Add(-time.Minute),
	}
	binding := domainnetworkaccess.NASBinding{RuntimeID: session.RuntimeID, NASID: session.NASID, SiteID: session.SiteID, Status: domainnetworkaccess.StatusActive, DisconnectSupported: true}
	input := domainnetworkaccess.SessionActionInput{Action: domainnetworkaccess.SessionActionCoA, TargetAccessProfile: domainnetworkaccess.ProfileRestricted, ReasonCode: "device_posture_changed"}

	plan, err := buildSessionActionPlan(now, session, binding, nil, input)
	if err != nil {
		t.Fatalf("buildSessionActionPlan() error = %v", err)
	}
	if plan.RequestedAction != domainnetworkaccess.SessionActionCoA || plan.EffectiveAction != domainnetworkaccess.SessionActionDisconnect || !plan.WillDisconnect || plan.PlanHash == "" {
		t.Fatalf("fallback plan = %#v", plan)
	}

	binding.CoASupported = true
	profile := domainnetworkaccess.SiteProfileBinding{SiteID: session.SiteID, AccessProfile: domainnetworkaccess.ProfileRestricted, VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 900}
	coaPlan, err := buildSessionActionPlan(now, session, binding, &profile, input)
	if err != nil {
		t.Fatalf("buildSessionActionPlan(CoA) error = %v", err)
	}
	if coaPlan.EffectiveAction != domainnetworkaccess.SessionActionCoA || coaPlan.WillDisconnect || coaPlan.RadiusAttributes == nil || coaPlan.RadiusAttributes.VLANID != 30 || coaPlan.PlanHash == plan.PlanHash {
		t.Fatalf("CoA plan = %#v", coaPlan)
	}
}
