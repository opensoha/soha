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

func writeHostDeploymentReceiptOutput(destination io.Writer, format string, receipt sohaapi.HostedAppDeploymentReceipt, idempotencyKey string) error {
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
	out.Printf("Status: %s\n", safeHostField(string(receipt.App.Status)))
	out.Printf("URL: %s\n", safeHostField(receipt.App.URL))
	effectiveKey := firstNonEmptyString(receipt.Revision.IdempotencyKey, idempotencyKey)
	if effectiveKey != "" {
		out.Printf("Idempotency Key: %s\n", safeHostField(effectiveKey))
	}
	if receipt.ReusedIdempotencyKey {
		out.Println("Reused Idempotency Key: true")
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
	out.Printf("Visibility: %s\n", formatHostVisibility(app.Visibility))
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
		out.Printf("%s\t%s\t%s\t%s\t%s\t%s\n",
			safeHostField(app.ID),
			safeHostField(string(app.Status)),
			formatHostVisibility(app.Visibility),
			firstNonEmptyString(safeHostField(app.ActiveRevisionID), "-"),
			safeHostField(app.Name),
			safeHostField(app.URL),
		)
	}
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
