package sohacli

import (
	"fmt"
	"io"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func safeHostField(value string) string {
	return terminalDisplayText(redactSensitiveText(strings.TrimSpace(value)))
}

func formatHostVisibility(scope sohaapi.HostedAppVisibilityScope) string {
	mode := safeHostField(string(scope.Mode))
	if scope.Mode != sohaapi.HostedAppVisibilityModeSelected {
		return mode
	}
	parts := make([]string, 0, 2)
	if len(scope.TeamIDs) > 0 {
		teams := make([]string, 0, len(scope.TeamIDs))
		for _, id := range scope.TeamIDs {
			teams = append(teams, safeHostField(id))
		}
		parts = append(parts, "teams="+strings.Join(teams, ","))
	}
	if len(scope.UserIDs) > 0 {
		users := make([]string, 0, len(scope.UserIDs))
		for _, id := range scope.UserIDs {
			users = append(users, safeHostField(id))
		}
		parts = append(parts, "users="+strings.Join(users, ","))
	}
	if len(parts) == 0 {
		return mode
	}
	return fmt.Sprintf("%s (%s)", mode, strings.Join(parts, " "))
}

func writeHostPreflightOutput(destination io.Writer, format string, bundle hostStaticBundle, result sohaapi.HostedAppPreflightResult) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(result))
	}
	out := newCheckedWriter(destination)
	verdict := "allowed"
	if !result.Allowed {
		verdict = "blocked"
	}
	out.Printf("Preflight: %s\n", verdict)
	out.Printf("App ID: %s\n", safeHostField(result.AppID))
	out.Printf("URL: %s\n", safeHostField(result.URL))
	out.Printf("Bundle: files=%d uncompressedBytes=%d compressedBytes=%d sha256=%s\n",
		bundle.FileCount, bundle.UncompressedBytes, bundle.CompressedBytes, bundle.BundleSHA256)
	out.Printf("Limits: maxFiles=%d maxUncompressedBytes=%d maxCompressedBytes=%d\n",
		result.MaxFileCount, result.MaxUncompressedBytes, result.MaxCompressedBytes)
	for _, warning := range result.Warnings {
		out.Printf("Warning: %s\n", safeHostField(warning))
	}
	for _, blocker := range result.Blockers {
		out.Printf("Blocker: %s\n", safeHostField(blocker))
	}
	return out.Err()
}

func writeHostDeploymentReceiptOutput(destination io.Writer, format string, receipt sohaapi.HostedAppDeploymentReceipt, idempotencyKey, note string) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(receipt))
	}
	out := newCheckedWriter(destination)
	out.Printf("App ID: %s\n", safeHostField(receipt.App.ID))
	out.Printf("Name: %s\n", safeHostField(receipt.App.Name))
	if receipt.Revision.RevisionNumber > 0 {
		out.Printf("Revision: %s (r%d)\n", safeHostField(receipt.Revision.ID), receipt.Revision.RevisionNumber)
	} else {
		out.Printf("Revision: %s\n", safeHostField(receipt.Revision.ID))
	}
	out.Printf("Visibility: %s\n", formatHostVisibility(receipt.App.Visibility))
	if receipt.App.PendingVisibilityApproval != nil {
		out.Printf("Pending Visibility Approval: %s\n", formatHostPendingApprovalDetail(*receipt.App.PendingVisibilityApproval))
	}
	if collaborators := effectiveHostCollaborators(receipt.App); len(collaborators) > 0 {
		out.Printf("Maintainers: %s\n", formatHostUserSummaries(collaborators))
	}
	out.Printf("Status: %s\n", safeHostField(string(receipt.App.Status)))
	out.Printf("URL: %s\n", safeHostField(receipt.App.URL))
	effectiveKey := firstNonEmptyString(receipt.Revision.IdempotencyKey, idempotencyKey)
	if effectiveKey != "" {
		out.Printf("Idempotency Key: %s\n", safeHostField(effectiveKey))
	}
	if receipt.ReusedIdempotencyKey {
		out.Println("Reused Idempotency Key: true")
	}
	if strings.TrimSpace(note) != "" {
		out.Printf("Note: %s\n", safeHostField(note))
	}
	return out.Err()
}

func writeHostedAppDetailOutput(destination io.Writer, format string, app sohaapi.HostedApp, note string) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(app))
	}
	out := newCheckedWriter(destination)
	out.Printf("App ID: %s\n", safeHostField(app.ID))
	out.Printf("Name: %s\n", safeHostField(app.Name))
	out.Printf("Kind: %s\n", safeHostField(string(app.Kind)))
	out.Printf("Status: %s\n", safeHostField(string(app.Status)))
	if app.OwnerProfile != nil {
		out.Printf("Owner: %s\n", formatHostUserSummary(*app.OwnerProfile))
	}
	out.Printf("Visibility: %s\n", formatHostVisibility(app.Visibility))
	if app.PendingVisibilityApproval != nil {
		out.Printf("Pending Visibility Approval: %s\n", formatHostPendingApprovalDetail(*app.PendingVisibilityApproval))
	}
	if collaborators := effectiveHostCollaborators(app); len(collaborators) > 0 {
		out.Printf("Maintainers: %s\n", formatHostUserSummaries(collaborators))
	}
	out.Printf("SPA Fallback: %t\n", app.SpaFallback)
	out.Printf("Active Revision: %s\n", firstNonEmptyString(safeHostField(app.ActiveRevisionID), "-"))
	out.Printf("Route Generation: %d\n", app.RouteGeneration)
	out.Printf("Host: %s\n", safeHostField(app.Host))
	out.Printf("URL: %s\n", safeHostField(app.URL))
	if strings.TrimSpace(app.SuspendedReason) != "" {
		out.Printf("Suspended Reason: %s\n", safeHostField(app.SuspendedReason))
	}
	if strings.TrimSpace(app.Description) != "" {
		out.Printf("Description: %s\n", safeHostField(app.Description))
	}
	if strings.TrimSpace(note) != "" {
		out.Printf("Note: %s\n", safeHostField(note))
	}
	return out.Err()
}

func writeHostedAppListOutput(destination io.Writer, format string, list sohaapi.HostedAppList) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(list))
	}
	out := newCheckedWriter(destination)
	out.Printf("Total: %d\n", list.Total)
	for _, app := range list.Items {
		cols := []string{
			safeHostField(app.ID),
			safeHostField(string(app.Status)),
			formatHostVisibility(app.Visibility),
			firstNonEmptyString(safeHostField(app.ActiveRevisionID), "-"),
			safeHostField(app.Name),
			safeHostField(app.URL),
		}
		if collaborators := effectiveHostCollaborators(app); len(collaborators) > 0 {
			cols = append(cols, "maintainers="+formatHostUserSummariesCompact(collaborators))
		}
		if pending := formatHostPendingApprovalCompact(app.PendingVisibilityApproval); pending != "" {
			cols = append(cols, "pendingApproval="+pending)
		}
		out.Println(strings.Join(cols, "\t"))
	}
	return out.Err()
}

func effectiveHostCollaborators(app sohaapi.HostedApp) []sohaapi.HostedAppUserSummary {
	ownerID := strings.TrimSpace(app.OwnerUserID)
	seen := make(map[string]struct{}, len(app.Maintainers)+len(app.MaintainerUserIDs))
	out := make([]sohaapi.HostedAppUserSummary, 0, len(app.Maintainers)+len(app.MaintainerUserIDs))
	for _, m := range app.Maintainers {
		uid := strings.TrimSpace(m.UserID)
		if uid == "" || uid == ownerID {
			continue
		}
		if _, exists := seen[uid]; exists {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, m)
	}
	for _, rawID := range app.MaintainerUserIDs {
		uid := strings.TrimSpace(rawID)
		if uid == "" || uid == ownerID {
			continue
		}
		if _, exists := seen[uid]; exists {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, sohaapi.HostedAppUserSummary{UserID: uid})
	}
	return out
}

func formatHostUserSummary(u sohaapi.HostedAppUserSummary) string {
	uid := safeHostField(u.UserID)
	name := safeHostField(u.DisplayName)
	teams := cleanHostTeamNames(u.TeamNames)
	base := uid
	if name != "" && uid != "" && name != uid {
		base = fmt.Sprintf("%s (%s)", name, uid)
	} else if name != "" {
		base = name
	}
	if len(teams) == 0 {
		return base
	}
	return fmt.Sprintf("%s [teams=%s]", base, strings.Join(teams, ","))
}

func formatHostUserSummaries(items []sohaapi.HostedAppUserSummary) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, formatHostUserSummary(item))
	}
	return strings.Join(parts, "; ")
}

func formatHostUserSummariesCompact(items []sohaapi.HostedAppUserSummary) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		uid := safeHostField(item.UserID)
		name := safeHostField(item.DisplayName)
		if name != "" && uid != "" && name != uid {
			parts = append(parts, fmt.Sprintf("%s(%s)", name, uid))
		} else {
			parts = append(parts, firstNonEmptyString(uid, name))
		}
	}
	return strings.Join(parts, ",")
}

func cleanHostTeamNames(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if safe := safeHostField(item); safe != "" {
			out = append(out, safe)
		}
	}
	return out
}

func formatHostPendingApprovalDetail(approval sohaapi.HostedAppVisibilityApproval) string {
	id := firstNonEmptyString(safeHostField(approval.ID), "-")
	status := firstNonEmptyString(safeHostField(string(approval.Status)), "-")
	requested := firstNonEmptyString(safeHostField(string(approval.RequestedVisibility.Mode)), "-")
	current := firstNonEmptyString(safeHostField(string(approval.CurrentVisibility.Mode)), "-")
	if reason := safeHostField(approval.Reason); reason != "" {
		return fmt.Sprintf("%s (status=%s requested=%s current=%s reason=%q)", id, status, requested, current, reason)
	}
	return fmt.Sprintf("%s (status=%s requested=%s current=%s)", id, status, requested, current)
}

func formatHostPendingApprovalCompact(approval *sohaapi.HostedAppVisibilityApproval) string {
	if approval == nil {
		return ""
	}
	id := firstNonEmptyString(safeHostField(approval.ID), "-")
	status := firstNonEmptyString(safeHostField(string(approval.Status)), "-")
	requested := firstNonEmptyString(safeHostField(string(approval.RequestedVisibility.Mode)), "-")
	return fmt.Sprintf("%s(%s->%s)", id, status, requested)
}

func formatPendingApprovalSubmissionNote(app sohaapi.HostedApp) string {
	if app.PendingVisibilityApproval == nil || app.PendingVisibilityApproval.Status != sohaapi.HostedAppVisibilityApprovalStatusPending {
		return ""
	}
	approvalID := firstNonEmptyString(safeHostField(app.PendingVisibilityApproval.ID), "-")
	effectiveMode := firstNonEmptyString(
		safeHostField(string(app.Visibility.Mode)),
		safeHostField(string(app.PendingVisibilityApproval.CurrentVisibility.Mode)),
	)
	return fmt.Sprintf("已提交全公司公开审批申请（单号 %s），审批通过前继续维持当前可见范围 %s", approvalID, effectiveMode)
}

func writeHostMaintainersOutput(destination io.Writer, format string, app sohaapi.HostedApp) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(app))
	}
	out := newCheckedWriter(destination)
	out.Printf("App ID: %s\n", safeHostField(app.ID))
	out.Printf("Name: %s\n", safeHostField(app.Name))
	if app.OwnerProfile != nil {
		out.Printf("Owner: %s\n", formatHostUserSummary(*app.OwnerProfile))
	} else if strings.TrimSpace(app.OwnerUserID) != "" {
		out.Printf("Owner: %s\n", safeHostField(app.OwnerUserID))
	}
	out.Printf("Visibility: %s\n", formatHostVisibility(app.Visibility))
	collaborators := effectiveHostCollaborators(app)
	if len(collaborators) == 0 {
		out.Println("Maintainers: none")
	} else {
		out.Printf("Maintainers (%d):\n", len(collaborators))
		for _, m := range collaborators {
			teams := "-"
			if cleaned := cleanHostTeamNames(m.TeamNames); len(cleaned) > 0 {
				teams = strings.Join(cleaned, ",")
			}
			out.Printf("  %s\t%s\t%s\n",
				safeHostField(m.UserID),
				firstNonEmptyString(safeHostField(m.DisplayName), "-"),
				teams,
			)
		}
	}
	out.Println("Note: 协作者仅具备更新版本与回滚权限，访问仍由可见范围控制 (collaborator maintainers can only upload revisions and roll back; visitor access remains controlled by visibility).")
	return out.Err()
}

func writeHostDeleteOutput(destination io.Writer, format string, result sohaapi.DeleteHostedAppResult, fallbackAppID, confirmedName string) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(result))
	}
	out := newCheckedWriter(destination)
	out.Printf("App ID: %s\n", safeHostField(firstNonEmptyString(result.AppID, fallbackAppID)))
	out.Printf("Confirmed Name: %s\n", safeHostField(confirmedName))
	out.Printf("Deleted: %t\n", result.Deleted)
	return out.Err()
}

func writeHostedAppRevisionListOutput(destination io.Writer, format string, list sohaapi.HostedAppRevisionList) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(list))
	}
	out := newCheckedWriter(destination)
	out.Printf("Total: %d\n", list.Total)
	for _, rev := range list.Items {
		out.Printf("%s\tr%d\t%s\tfiles=%d\tbytes=%d\tspa=%t\t%s\n",
			safeHostField(rev.ID),
			rev.RevisionNumber,
			safeHostField(string(rev.Status)),
			rev.FileCount,
			rev.TotalBytes,
			rev.SpaFallback,
			rev.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		)
	}
	return out.Err()
}

func writeHostedAppRevisionDetailOutput(destination io.Writer, format string, rev sohaapi.HostedAppRevision) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(rev))
	}
	out := newCheckedWriter(destination)
	out.Printf("Revision ID: %s\n", safeHostField(rev.ID))
	out.Printf("App ID: %s\n", safeHostField(rev.AppID))
	out.Printf("Revision Number: %d\n", rev.RevisionNumber)
	out.Printf("Kind: %s\n", safeHostField(string(rev.Kind)))
	out.Printf("Status: %s\n", safeHostField(string(rev.Status)))
	out.Printf("SPA Fallback: %t\n", rev.SpaFallback)
	out.Printf("Files: %d\n", rev.FileCount)
	out.Printf("Total Bytes: %d\n", rev.TotalBytes)
	out.Printf("Bundle SHA256: %s\n", safeHostField(rev.BundleSha256))
	out.Printf("Created By: %s\n", safeHostField(rev.CreatedBy))
	out.Printf("Created At: %s\n", rev.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	if strings.TrimSpace(rev.Description) != "" {
		out.Printf("Description: %s\n", safeHostField(rev.Description))
	}
	if strings.TrimSpace(rev.FailureStage) != "" || strings.TrimSpace(rev.FailureReason) != "" {
		out.Printf("Failure: stage=%s reason=%s\n", safeHostField(rev.FailureStage), safeHostField(rev.FailureReason))
	}
	return out.Err()
}

func writeHostAudienceOutput(destination io.Writer, format string, candidates sohaapi.AppHostingAudienceCandidates) error {
	if format == "json" {
		return writePrettyJSON(destination, sanitizeCLIJSONValue(candidates))
	}
	out := newCheckedWriter(destination)
	if len(candidates.Teams) == 0 && len(candidates.Users) == 0 {
		out.Println("No matching audience candidates.")
		return out.Err()
	}
	for _, team := range candidates.Teams {
		out.Printf("team\t%s\t%s\t%s\n",
			safeHostField(team.ID),
			safeHostField(team.Name),
			firstNonEmptyString(safeHostField(team.Slug), "-"),
		)
	}
	for _, user := range candidates.Users {
		out.Printf("user\t%s\t%s\t%s\t%s\n",
			safeHostField(user.ID),
			safeHostField(user.Username),
			safeHostField(user.DisplayName),
			firstNonEmptyString(safeHostField(user.Department), "-"),
		)
	}
	return out.Err()
}
