package apphosting

import (
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
)

// Route generation changes on explicit audience updates, even when the selected range
// is already private. A delayed publication must not undo that newer decision.
func publicationVisibilityInput(current domain.App, previousGeneration int64, rev domain.Revision, settings domain.Settings, now time.Time) (app.UpdateVisibilityTxInput, bool) {
	intent := rev.PublishVisibility
	if intent == nil || intent.BaseRouteGeneration != previousGeneration || !current.IsOwner(rev.CreatedBy) {
		return app.UpdateVisibilityTxInput{}, false
	}
	if intent.Scope.Mode == sohaapi.HostedAppVisibilityModeOrganization &&
		settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyDisabled {
		return app.UpdateVisibilityTxInput{}, false
	}
	return app.UpdateVisibilityTxInput{
		AppID: current.ID, ActorUserID: rev.CreatedBy, RequestedScope: intent.Scope, Reason: intent.Reason,
		NewApprovalID: "apv_" + strings.TrimPrefix(rev.ID, "rev_"), Now: now,
	}, true
}
