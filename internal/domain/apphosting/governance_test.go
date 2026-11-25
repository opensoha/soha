package apphosting

import (
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func TestOwnerQuotaByKindAndMembership(t *testing.T) {
	settings := DefaultSettings()
	settings.MaxAppsPerOwner = 3
	if got := settings.OwnerQuota(sohaapi.HostedAppKindDynamic, nil, nil); got != 3 {
		t.Fatalf("legacy dynamic cap = %d, want 3", got)
	}

	governance := settings.EffectiveGovernance()
	governance.MaxDynamicAppsPerOwner = 5
	governance.QuotaOverrides = []sohaapi.AppHostingQuotaOverride{
		{Scope: sohaapi.AppHostingQuotaOverrideScopeTeam, ID: "finance", MaxStaticApps: 10, MaxDynamicApps: 8},
		{Scope: sohaapi.AppHostingQuotaOverrideScopeRole, ID: "auditor", MaxStaticApps: 4},
	}
	settings.Governance = &governance
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name         string
		kind         sohaapi.HostedAppKind
		teams, roles []string
		want         int32
	}{
		{"default static", sohaapi.HostedAppKindStatic, nil, nil, 3},
		{"default dynamic", sohaapi.HostedAppKindDynamic, nil, []string{"finance"}, 5},
		{"team can raise limit", sohaapi.HostedAppKindStatic, []string{"finance"}, nil, 10},
		{"lowest matching static", sohaapi.HostedAppKindStatic, []string{"finance"}, []string{"auditor"}, 4},
		{"absent dynamic override inherits", sohaapi.HostedAppKindDynamic, []string{"finance"}, []string{"auditor"}, 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := settings.OwnerQuota(tt.kind, tt.teams, tt.roles); got != tt.want {
				t.Fatalf("quota = %d, want %d", got, tt.want)
			}
		})
	}
	governance.QuotaOverrides = append(governance.QuotaOverrides, governance.QuotaOverrides[0])
	if err := settings.Validate(); err == nil {
		t.Fatal("duplicate team override was accepted")
	}
}
