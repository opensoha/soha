package sohacli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"sort"
	"strings"
	"time"
)

type pluginRepositoryInstallOptions struct {
	RepositoryURL string
	Ref           string
	Enable        bool
	Yes           bool
	JSON          bool
}

type pluginRepositoryUpgradeOptions struct {
	Ref                       string
	ConfirmPermissionIncrease bool
	Yes                       bool
	JSON                      bool
}

func runPluginRepositoryInstall(ctx context.Context, rt Runtime, profile ProfileConfig, options pluginRepositoryInstallOptions) error {
	client := gatewayClient(rt, profile)
	headers := gatewayHeaders(profile, "", "", "", "soha-cli")
	preview, err := previewRepositoryPlugin(ctx, rt, client, PluginRepositoryPreviewRequest{
		RepositoryURL: strings.TrimSpace(options.RepositoryURL),
		Ref:           strings.TrimSpace(options.Ref),
	}, headers)
	if err != nil {
		return repositoryPluginEndpointError(err)
	}
	if strings.TrimSpace(preview.PreviewToken) == "" {
		return fmt.Errorf("repository preview did not return a confirmation token")
	}
	if err := writePluginRepositoryPreview(rt, preview, options.JSON); err != nil {
		return err
	}
	if err := confirmRepositoryAction(rt, options.Yes, "Install "+repositoryPreviewTarget(preview)+" from repository preview?"); err != nil {
		return err
	}
	item, err := client.InstallRepositoryPlugin(ctx, PluginRepositoryInstallRequest{
		PreviewToken: preview.PreviewToken,
		Enable:       options.Enable,
	}, headers)
	if err != nil {
		return repositoryPluginEndpointError(redactPluginPreviewToken(err, preview.PreviewToken))
	}
	if options.JSON {
		return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
	}
	_, err = fmt.Fprintf(rt.Out, "Installed plugin %s %s (%s)\n", pluginPreviewText(item.ID), pluginPreviewText(item.Version), pluginPreviewText(item.Status))
	return err
}

func runPluginRepositoryUpgrade(ctx context.Context, rt Runtime, profile ProfileConfig, pluginID string, options pluginRepositoryUpgradeOptions) error {
	client := gatewayClient(rt, profile)
	headers := gatewayHeaders(profile, "", "", "", "soha-cli")
	installed, err := client.GetInstalledPlugin(ctx, pluginID, headers)
	if err != nil {
		return repositoryPluginEndpointError(err)
	}
	input := PluginRepositoryPreviewRequest{Ref: strings.TrimSpace(options.Ref)}
	if installed.SourceInfo != nil {
		switch installed.SourceInfo.Kind {
		case "github-repo":
			input.RepositoryURL = strings.TrimSpace(installed.SourceInfo.CanonicalSource)
		case "marketplace":
			input.RepositoryURL = strings.TrimSpace(installed.SourceInfo.RepositoryURL)
			input.MarketplaceSourceID = strings.TrimSpace(installed.SourceInfo.SourceID)
			input.PluginID = pluginID
		}
	}
	if input.RepositoryURL == "" || (input.PluginID != "" && input.MarketplaceSourceID == "") {
		return fmt.Errorf("plugin %s is not installed from a repository source; repository upgrade is unavailable and no legacy upgrade was attempted", terminalLabel(pluginID))
	}
	preview, err := previewRepositoryPlugin(ctx, rt, client, input, headers)
	if err != nil {
		return repositoryPluginEndpointError(err)
	}
	if strings.TrimSpace(preview.PreviewToken) == "" {
		return fmt.Errorf("repository preview did not return a confirmation token")
	}
	latest, err := client.GetInstalledPlugin(ctx, pluginID, headers)
	if err != nil {
		return repositoryPluginEndpointError(err)
	}
	if latest.SourceInfo == nil || *latest.SourceInfo != *installed.SourceInfo ||
		input.RepositoryURL != preview.SourceInfo.CanonicalSource ||
		preview.Manifest.ID != pluginID || !latest.InstalledAt.Equal(installed.InstalledAt) || !latest.UpdatedAt.Equal(installed.UpdatedAt) {
		return fmt.Errorf("the installed plugin changed during repository preview; run the upgrade again")
	}
	if err := writePluginRepositoryPreview(rt, preview, options.JSON); err != nil {
		return err
	}
	addedRequired, addedDomain := addedPluginPermissions(preview.Manifest.Permissions, installedPluginPermissions(installed))
	if len(addedRequired) > 0 || len(addedDomain) > 0 {
		if err := writeAddedPluginPermissions(rt, addedRequired, addedDomain, options.JSON); err != nil {
			return err
		}
	}
	if err := confirmRepositoryAction(rt, options.Yes, "Upgrade "+repositoryPreviewTarget(preview)+" from repository preview?"); err != nil {
		return err
	}
	confirmPermissionIncrease := false
	if len(addedRequired) > 0 || len(addedDomain) > 0 {
		if options.ConfirmPermissionIncrease {
			confirmPermissionIncrease = true
		} else {
			if !terminalInput(rt) {
				return usageError{message: "the repository upgrade adds permissions; rerun with --confirm-permission-increase after reviewing them"}
			}
			ok, err := confirmAction(rt, "Acknowledge the added permissions required="+strings.Join(pluginPreviewTexts(addedRequired), ",")+" domain="+strings.Join(pluginPreviewTexts(addedDomain), ",")+"?")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("action declined; added permissions were not acknowledged")
			}
			confirmPermissionIncrease = true
		}
	}
	item, err := client.UpgradeRepositoryPlugin(ctx, pluginID, PluginRepositoryUpgradeRequest{
		PreviewToken:              preview.PreviewToken,
		ConfirmPermissionIncrease: confirmPermissionIncrease,
	}, headers)
	if err != nil {
		return repositoryPluginEndpointError(redactPluginPreviewToken(err, preview.PreviewToken))
	}
	if options.JSON {
		return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
	}
	_, err = fmt.Fprintf(rt.Out, "Upgraded plugin %s to %s\n", pluginPreviewText(item.ID), pluginPreviewText(item.Version))
	return err
}

func previewRepositoryPlugin(ctx context.Context, rt Runtime, client APIClient, input PluginRepositoryPreviewRequest, headers map[string]string) (PluginRepositoryPreview, error) {
	preview, err := client.PreviewRepositoryPlugin(ctx, input, headers)
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) || statusErr.Code != "plugin_repository_branch_required" {
		return preview, err
	}
	if !terminalInput(rt) {
		return preview, usageError{message: "repository requires a branch; rerun with --ref refs/heads/<branch>"}
	}
	ref, err := selectRepositoryBranch(ctx, rt, client, input.RepositoryURL, headers)
	if err != nil {
		return preview, err
	}
	input.Ref = "refs/heads/" + ref
	return client.PreviewRepositoryPlugin(ctx, input, headers)
}

func selectRepositoryBranch(ctx context.Context, rt Runtime, client APIClient, repositoryURL string, headers map[string]string) (string, error) {
	for page := 1; page <= 1000; page++ {
		var result itemResponse[sohaapi.PluginRepositoryRefList]
		input := sohaapi.PluginRepositoryRefsRequest{RepositoryURL: repositoryURL, Kind: sohaapi.PluginRepositoryRefsRequestKindBranch, Page: &page}
		if err := client.doJSON(ctx, http.MethodPost, "/api/v1/plugins/repository/refs", client.Token, headers, input, &result); err != nil {
			return "", err
		}
		out := newCheckedWriter(rt.Err)
		out.Println("Select a repository branch:")
		for i, ref := range result.Data.Refs {
			out.Printf("  %d) %s\n", i+1, pluginPreviewText(ref.Name))
		}
		if result.Data.HasMore {
			out.Print("Enter a number or n for next page: ")
		} else {
			out.Print("Enter a number: ")
		}
		if err := out.Err(); err != nil {
			return "", err
		}
		// Read only this line so a subsequent confirmation remains in stdin.
		var line strings.Builder
		var b [1]byte
		for line.Len() < 64 {
			_, err := rt.In.Read(b[:])
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return "", err
			}
			if b[0] == '\n' {
				break
			}
			line.WriteByte(b[0])
		}
		choice := strings.TrimSpace(line.String())
		if choice == "n" && result.Data.HasMore {
			continue
		}
		index, err := strconv.Atoi(choice)
		if err != nil || index < 1 || index > len(result.Data.Refs) {
			return "", fmt.Errorf("no valid branch selected; rerun with --ref refs/heads/<branch>")
		}
		return result.Data.Refs[index-1].Name, nil
	}
	return "", fmt.Errorf("no branch selected; rerun with --ref refs/heads/<branch>")
}

func writePluginRepositoryPreview(rt Runtime, preview PluginRepositoryPreview, jsonOutput bool) error {
	if jsonOutput {
		document, err := pluginRepositoryPreviewDocument(preview)
		if err != nil {
			return err
		}
		return writePrettyJSON(rt.Err, document)
	}
	out := newCheckedWriter(rt.Out)
	manifest := preview.Manifest
	out.Printf("repository: %s\ncommit: %s\nofficial: %t\nmanifest digest: %s\nchecksum: %s\nsignature: %s\nexpires: %s\n",
		pluginPreviewText(preview.SourceInfo.CanonicalSource),
		pluginPreviewText(preview.SourceInfo.CommitSha),
		preview.SourceInfo.Official,
		pluginPreviewText(preview.SourceInfo.RawManifestChecksum),
		pluginPreviewText(preview.ChecksumStatus),
		pluginPreviewText(preview.SignatureStatus),
		preview.ExpiresAt.UTC().Format(time.RFC3339),
	)
	out.Printf("plugin: %s %s (%s)\npublisher: %s\ntype: %s\nname: %s\n",
		pluginPreviewText(manifest.ID),
		pluginPreviewText(manifest.Version),
		pluginPreviewText(manifest.Description),
		pluginPreviewText(manifest.Publisher),
		pluginPreviewText(manifest.Type),
		pluginPreviewText(manifest.Name),
	)
	if manifest.Permissions != nil {
		out.Printf("permissions: required=%s domain=%s\n",
			strings.Join(pluginPreviewTexts(manifest.Permissions.Required), ","),
			strings.Join(pluginPreviewTexts(manifest.Permissions.Domain), ","),
		)
	}
	if manifest.Secrets != nil && len(manifest.Secrets.Required) > 0 {
		names := make([]string, 0, len(manifest.Secrets.Required))
		for _, requirement := range manifest.Secrets.Required {
			names = append(names, pluginPreviewText(requirement.Name))
		}
		sort.Strings(names)
		out.Printf("secrets: required=%s\n", strings.Join(names, ","))
	}
	return out.Err()
}

func pluginRepositoryPreviewDocument(preview PluginRepositoryPreview) (map[string]any, error) {
	raw, err := json.Marshal(preview)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	delete(document, "previewToken")
	sanitized, ok := sanitizeCLIJSONValue(document).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cannot sanitize repository preview")
	}
	display, ok := terminalSafePluginPreview(sanitized).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cannot format repository preview")
	}
	return display, nil
}

func terminalSafePluginPreview(value any) any {
	switch typed := value.(type) {
	case string:
		return terminalDisplayText(typed)
	case []any:
		for index := range typed {
			typed[index] = terminalSafePluginPreview(typed[index])
		}
		return typed
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[terminalDisplayText(key)] = terminalSafePluginPreview(item)
		}
		return out
	default:
		return value
	}
}

func writeAddedPluginPermissions(rt Runtime, required, domain []string, jsonOutput bool) error {
	if !jsonOutput {
		_, err := fmt.Fprintf(rt.Out, "added permissions: required=%s domain=%s\n",
			strings.Join(pluginPreviewTexts(required), ","),
			strings.Join(pluginPreviewTexts(domain), ","),
		)
		return err
	}
	_, err := fmt.Fprintf(rt.Err, "added permissions: required=%s domain=%s\n",
		strings.Join(pluginPreviewTexts(required), ","),
		strings.Join(pluginPreviewTexts(domain), ","),
	)
	return err
}

func confirmRepositoryAction(rt Runtime, yes bool, description string) error {
	if yes {
		return nil
	}
	if !terminalInput(rt) {
		return usageError{message: "repository install or upgrade requires --yes in non-interactive mode after reviewing the preview"}
	}
	ok, err := confirmAction(rt, description)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("action declined; no repository change was requested")
	}
	return nil
}

func addedPluginPermissions(next, current *PluginPermissionRequest) ([]string, []string) {
	return addedPermissionNames(pluginPermissionList(next, true), pluginPermissionList(current, true)),
		addedPermissionNames(pluginPermissionList(next, false), pluginPermissionList(current, false))
}

func installedPluginPermissions(installed InstalledPlugin) *PluginPermissionRequest {
	if installed.RequestedPermissions != nil {
		return installed.RequestedPermissions
	}
	return installed.Manifest.Permissions
}

func pluginPermissionList(permissions *PluginPermissionRequest, required bool) []string {
	if permissions == nil {
		return nil
	}
	if required {
		return permissions.Required
	}
	return permissions.Domain
}

func addedPermissionNames(next, current []string) []string {
	existing := make(map[string]struct{}, len(current))
	for _, name := range current {
		existing[strings.TrimSpace(name)] = struct{}{}
	}
	added := make([]string, 0)
	for _, name := range next {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := existing[name]; !ok {
			added = append(added, name)
			existing[name] = struct{}{}
		}
	}
	sort.Strings(added)
	return added
}

func repositoryPreviewTarget(preview PluginRepositoryPreview) string {
	return pluginPreviewText(preview.Manifest.ID) + " " + pluginPreviewText(preview.Manifest.Version) + " at " + pluginPreviewText(preview.SourceInfo.CommitSha)
}

func repositoryPluginEndpointError(err error) error {
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) && ((statusErr.StatusCode == http.StatusNotFound && statusErr.Code == "") ||
		statusErr.StatusCode == http.StatusMethodNotAllowed || statusErr.StatusCode == http.StatusNotImplemented) {
		return fmt.Errorf("repository plugin endpoints are unavailable on this server; no legacy fallback was attempted: %w", err)
	}
	return err
}

type redactedPluginTokenError struct {
	err   error
	token string
}

func (e redactedPluginTokenError) Error() string {
	if e.token == "" {
		return e.err.Error()
	}
	return strings.ReplaceAll(e.err.Error(), e.token, "[REDACTED]")
}

func (e redactedPluginTokenError) Unwrap() error { return e.err }

func redactPluginPreviewToken(err error, token string) error {
	if err == nil || strings.TrimSpace(token) == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return redactedPluginTokenError{err: err, token: token}
}

func pluginPreviewText(value string) string {
	return terminalDisplayText(redactSensitiveText(value))
}

func pluginPreviewTexts(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, pluginPreviewText(value))
	}
	return out
}

func validateRepositoryInstallFlags(fs *flag.FlagSet, repositoryURL string) error {
	if strings.TrimSpace(repositoryURL) == "" {
		return usageError{message: "--repository requires a public GitHub repository URL"}
	}
	if fs.NArg() > 0 {
		return usageError{message: "a plugin id cannot be combined with --repository; the preview identifies the plugin"}
	}
	return rejectPluginFlags(fs, "manifest", "source", "marketplace", "source-id", "version", "expected-checksum")
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(item *flag.Flag) {
		if item.Name == name {
			set = true
		}
	})
	return set
}

func rejectPluginFlags(fs *flag.FlagSet, names ...string) error {
	rejected := map[string]bool{}
	for _, name := range names {
		rejected[name] = true
	}
	var found string
	fs.Visit(func(item *flag.Flag) {
		if found == "" && rejected[item.Name] {
			found = item.Name
		}
	})
	if found != "" {
		return usageError{message: "--" + found + " is not valid with the selected plugin source"}
	}
	return nil
}
