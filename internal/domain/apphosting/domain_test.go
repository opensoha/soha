package apphosting

import (
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestReservedDNSLabelsAndBuilder(t *testing.T) {
	t.Parallel()

	reserved := []string{
		"auth", "LOGIN", "Logout", "admin", "api", "portal", "static", "assets",
		"sso", "console", "www", "mail", "status", "health", "_soha", "soha", "internal", "gateway",
	}
	for _, word := range reserved {
		if !IsReservedDNSLabel(word) {
			t.Fatalf("expected %q to be reserved", word)
		}
		if err := ValidateDNSLabel(word); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("ValidateDNSLabel(%q) = %v, want ErrInvalidArgument", word, err)
		}
		if _, err := AllocateDNSLabel(word, "My App"); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("AllocateDNSLabel(%q) = %v, want ErrInvalidArgument", word, err)
		}
	}

	// Reserved name falls back to safe "app-<4hex>" when slug is derived automatically.
	derived, err := AllocateDNSLabel("", "Portal")
	if err != nil || !strings.HasPrefix(derived, "app-") || len(derived) != 8 {
		t.Fatalf("AllocateDNSLabel('', 'Portal') = %q, %v", derived, err)
	}

	built, err := BuildDNSLabel("quarterly-report", "a1f9")
	if err != nil || built != "quarterly-report-a1f9" {
		t.Fatalf("BuildDNSLabel = %q, %v", built, err)
	}
}

func TestParseHostedAppHostAndReturnPath(t *testing.T) {
	t.Parallel()

	label, host, err := ParseHostedAppHost("Sales-Dash-A1B2.Apps.Local:18090", "apps.local")
	if err != nil || label != "sales-dash-a1b2" || host != "sales-dash-a1b2.apps.local:18090" {
		t.Fatalf("ParseHostedAppHost with port = (%q, %q, %v)", label, host, err)
	}

	for _, bad := range []string{
		"",
		"127.0.0.1:18090",
		"auth.apps.local",
		"_soha.apps.local:18090",
		"nested.sub.apps.local",
		"sales-dash-a1b2.other.local",
		"sales-dash-a1b2.apps.local:99999",
	} {
		if _, _, err := ParseHostedAppHost(bad, "apps.local"); err == nil {
			t.Fatalf("expected ParseHostedAppHost(%q) to fail", bad)
		}
	}

	if got, err := ValidateReturnPath(""); err != nil || got != "/" {
		t.Fatalf("ValidateReturnPath('') = %q, %v", got, err)
	}
	if got, err := ValidateReturnPath("/reports/q4?tab=summary"); err != nil || got != "/reports/q4?tab=summary" {
		t.Fatalf("ValidateReturnPath valid = %q, %v", got, err)
	}
	for _, badPath := range []string{"//evil.example", `/\evil.example`, "https://evil.example", "relative", "/bad\r\nheader"} {
		if _, err := ValidateReturnPath(badPath); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("ValidateReturnPath(%q) = %v, want ErrInvalidArgument", badPath, err)
		}
	}
}

func TestCanAccessAndVisibilityNormalization(t *testing.T) {
	t.Parallel()

	settings := DefaultSettings()
	settings.Enabled = true
	settings.AppsDomain = "apps.local"
	settings.StorageIntegrationID = "int_s3"

	baseApp := App{
		ID:                "app_1",
		OwnerUserID:       "usr_owner",
		MaintainerUserIDs: []string{"usr_maint"},
		Status:            sohaapi.HostedAppStatusOnline,
		ActiveRevisionID:  "rev_1",
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
	}

	owner := domainidentity.Principal{UserID: "usr_owner", Teams: []string{"team_eng"}}
	maint := domainidentity.Principal{UserID: "usr_maint", Teams: []string{"team_eng"}}
	financeColleague := domainidentity.Principal{UserID: "usr_fin", Teams: []string{"team_finance"}}
	stranger := domainidentity.Principal{UserID: "usr_other", Teams: []string{"team_hr"}}

	// Private: ONLY owner has content access (not even maintainer unless explicitly shared).
	if !CanAccess(owner, baseApp, settings) {
		t.Fatal("owner should access private online app")
	}
	if CanAccess(maint, baseApp, settings) || CanAccess(financeColleague, baseApp, settings) {
		t.Fatal("non-owner must not access private app")
	}

	// Selected: owner + selected user + selected cross-department team.
	selectedApp := baseApp
	vis, err := NormalizeVisibilityInput(&sohaapi.HostedAppVisibilityInput{
		Mode:    sohaapi.HostedAppVisibilityModeSelected,
		UserIDs: []string{"usr_maint"},
		TeamIDs: []string{"team_finance"},
	}, settings)
	if err != nil {
		t.Fatal(err)
	}
	selectedApp.Visibility = vis
	if !CanAccess(owner, selectedApp, settings) || !CanAccess(maint, selectedApp, settings) || !CanAccess(financeColleague, selectedApp, settings) {
		t.Fatal("expected owner, selected user, and selected team member to have access")
	}
	if CanAccess(stranger, selectedApp, settings) || CanAccess(domainidentity.Principal{}, selectedApp, settings) {
		t.Fatal("stranger or unauthenticated visitor must be denied")
	}

	// Offline / suspended / platform disabled immediately revokes everyone including owner.
	offlineApp := selectedApp
	offlineApp.Status = sohaapi.HostedAppStatusOffline
	if CanAccess(owner, offlineApp, settings) {
		t.Fatal("offline app must deny access")
	}
	disabledSettings := settings
	disabledSettings.Enabled = false
	if CanAccess(owner, selectedApp, disabledSettings) {
		t.Fatal("disabled platform must deny access")
	}

	// Disabling AllowOrganizationVisibility rejects new organization scope configuration.
	noOrgSettings := settings
	noOrgSettings.AllowOrganizationVisibility = false
	if _, err := NormalizeVisibilityInput(&sohaapi.HostedAppVisibilityInput{
		Mode: sohaapi.HostedAppVisibilityModeOrganization,
	}, noOrgSettings); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied when AllowOrganizationVisibility=false, got %v", err)
	}

	// Portal visibility never includes private apps even for the owner.
	if VisibleInPortal(baseApp, owner, settings) {
		t.Fatal("private app must never be visible in Colleague Works portal")
	}
	if !VisibleInPortal(selectedApp, owner, settings) || !VisibleInPortal(selectedApp, financeColleague, settings) || VisibleInPortal(selectedApp, stranger, settings) {
		t.Fatal("unexpected VisibleInPortal evaluation for selected app")
	}
}
