package sohacli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

var hostSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

func hostCommandSpec() commandSpec {
	return commandSpec{
		Name:    "host",
		Usage:   "soha host <deploy|list|get|config|revisions|rollback|visibility|maintainers|offline|online|delete|audience> [options]",
		Summary: "Deploy and manage workspace hosted web applications",
		Examples: []string{
			"soha host deploy ./dist --name \"Quarterly Report\" --dry-run --profile local",
			"soha host deploy ./dist --name \"Quarterly Report\" --yes --profile local",
			"soha host audience --query finance --profile local",
		},
		Subcommands: append(hostCoreSubcommandSpecs(), hostGovernanceSubcommandSpecs()...),
		Handler:     runHost,
	}
}

func hostCoreSubcommandSpecs() []commandSpec {
	return []commandSpec{
		{
			Name:    "deploy",
			Usage:   "soha host deploy [dir] [--kind static|dynamic] [--app-id ID] [--name NAME] [--slug SLUG] [--description TEXT] [--spa] [--visibility private|selected|organization] [--user ID]... [--team ID]... [--reason TEXT] [--idempotency-key KEY] [--dry-run] [--yes] [--profile NAME] [--output text|json]",
			Summary: "Scan, preflight, and upload a static web bundle or dynamic source to workspace App Hosting",
			Examples: []string{
				"soha host deploy ./dist --name \"Ops Dashboard\" --dry-run --profile local",
				"soha host deploy ./service --kind dynamic --name \"Ops API\" --dry-run --profile local",
				"soha host deploy ./dist --name \"Ops Dashboard\" --spa --yes --profile local",
				"soha host deploy ./dist --app-id app_123 --idempotency-key deploy-r2 --yes --profile local",
			},
		},
		{
			Name:    "list",
			Usage:   "soha host list [--scope mine|all] [--query TEXT] [--status STATUS] [--limit N] [--offset N] [--profile NAME] [--output text|json]",
			Summary: "List hosted applications in maintainer or administrator scope",
			Examples: []string{
				"soha host list --profile local",
				"soha host list --scope all --status online --output json --profile local",
			},
		},
		{
			Name:    "get",
			Usage:   "soha host get <app-id> [--profile NAME] [--output text|json]",
			Summary: "Get hosted application metadata, active revision, collaborators, pending approval, and fixed URL",
			Examples: []string{
				"soha host get app_123 --profile local",
				"soha host get app_123 --output json --profile local",
			},
		},
		{
			Name:    "config",
			Usage:   "soha host config <app-id> [--set-env NAME]... [--remove-env NAME]... [--yes] [--profile NAME] [--output text|json]",
			Summary: "Read masked runtime configuration or stage environment changes for a new revision",
			Examples: []string{
				"soha host config app_123 --profile local",
				"soha host config app_123 --set-env APP_TOKEN --yes --profile local",
			},
		},
		{
			Name:    "revisions",
			Usage:   "soha host revisions <app-id> [--limit N] [--offset N] [--profile NAME] [--output text|json]",
			Summary: "List immutable revisions of a hosted application",
			Examples: []string{
				"soha host revisions app_123 --limit 20 --profile local",
				"soha host revisions app_123 --output json --profile local",
			},
		},
		{
			Name:    "rollback",
			Usage:   "soha host rollback <app-id> --revision <revision-id> [--idempotency-key KEY] [--reason TEXT] [--yes] [--profile NAME] [--output text|json]",
			Summary: "Switch the active revision to an earlier ready revision without altering visibility",
			Examples: []string{
				"soha host rollback app_123 --revision rev_1 --reason \"revert broken chart\" --yes --profile local",
			},
		},
	}
}

func hostGovernanceSubcommandSpecs() []commandSpec {
	return []commandSpec{
		{
			Name:    "visibility",
			Usage:   "soha host visibility <app-id> (--mode <private|selected|organization> [--user ID]... [--team ID]... | --withdraw) [--reason TEXT] [--yes] [--profile NAME] [--output text|json]",
			Summary: "Update hosted application audience visibility, submit or withdraw organization visibility approval, and enforce immediate revocation",
			Examples: []string{
				"soha host visibility app_123 --mode private --yes --profile local",
				"soha host visibility app_123 --mode selected --team team_finance --user usr_1 --yes --profile local",
				"soha host visibility app_123 --mode organization --reason \"open quarterly metrics to all staff\" --yes --profile local",
				"soha host visibility app_123 --withdraw --reason \"narrow scope before review\" --profile local",
			},
		},
		{
			Name:    "maintainers",
			Usage:   "soha host maintainers <app-id> [--users ID[,ID...] | --clear] [--yes] [--profile NAME] [--output text|json]",
			Summary: "Inspect or replace collaborator maintainers (revision upload and rollback only; visitor access follows visibility)",
			Examples: []string{
				"soha host maintainers app_123 --profile local",
				"soha host maintainers app_123 --users usr_bob,usr_carol --profile local",
				"soha host maintainers app_123 --clear --profile local",
			},
		},
		{
			Name:    "offline",
			Usage:   "soha host offline <app-id> [--reason TEXT] [--yes] [--profile NAME] [--output text|json]",
			Summary: "Take a hosted application offline while retaining revisions",
			Examples: []string{
				"soha host offline app_123 --reason \"maintenance window\" --yes --profile local",
			},
		},
		{
			Name:    "online",
			Usage:   "soha host online <app-id> [--reason TEXT] [--yes] [--profile NAME] [--output text|json]",
			Summary: "Bring an offline hosted application with a ready revision back online",
			Examples: []string{
				"soha host online app_123 --yes --profile local",
			},
		},
		{
			Name:    "delete",
			Usage:   "soha host delete <app-id> --confirm <exact-app-name> [--reason TEXT] [--profile NAME] [--output text|json]",
			Summary: "Permanently delete a hosted application and its revisions after exact application name confirmation",
			Examples: []string{
				"soha host delete app_123 --confirm \"Quarterly Report\" --reason \"deprecated report\" --profile local",
			},
		},
		{
			Name:    "audience",
			Usage:   "soha host audience [--query TEXT] [--limit N] [--profile NAME] [--output text|json]",
			Summary: "Search sanitized colleague and department candidates for sharing",
			Examples: []string{
				"soha host audience --query finance --profile local",
				"soha host audience --query carol --output json --profile local",
			},
		},
	}
}

func runHost(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return usageError{message: "host requires a subcommand: deploy, list, get, config, revisions, rollback, visibility, maintainers, offline, online, delete, or audience"}
	}
	switch args[0] {
	case "deploy":
		return runHostDeploy(ctx, args[1:], rt)
	case "list":
		return runHostList(ctx, args[1:], rt)
	case "get":
		return runHostGet(ctx, args[1:], rt)
	case "config":
		return runHostConfig(ctx, args[1:], rt)
	case "revisions":
		return runHostRevisions(ctx, args[1:], rt)
	case "rollback":
		return runHostRollback(ctx, args[1:], rt)
	case "visibility":
		return runHostVisibility(ctx, args[1:], rt)
	case "maintainers":
		return runHostMaintainers(ctx, args[1:], rt)
	case "offline":
		return runHostLifecycle(ctx, "offline", args[1:], rt)
	case "online":
		return runHostLifecycle(ctx, "online", args[1:], rt)
	case "delete":
		return runHostDelete(ctx, args[1:], rt)
	case "audience":
		return runHostAudience(ctx, args[1:], rt)
	default:
		return usageError{message: fmt.Sprintf("unknown host command %q; expected deploy, list, get, config, revisions, rollback, visibility, maintainers, offline, online, delete, or audience", args[0])}
	}
}

type hostDeployOptions struct {
	dir              string
	appID            string
	name             string
	nameExplicit     bool
	slug             string
	description      string
	descExplicit     bool
	kind             sohaapi.HostedAppKind
	spa              bool
	spaExplicit      bool
	visibilityInput  *sohaapi.HostedAppVisibilityInput
	visibilityReason string
	idempotencyKey   string
	dryRun           bool
	yes              bool
	profile          string
	format           string
}

func parseHostDeployOptions(args []string, rt Runtime) (hostDeployOptions, error) {
	fs := newRuntimeFlagSet("host deploy", args, rt)
	appID := fs.String("app-id", "", "existing hosted app ID to update")
	name := fs.String("name", "", "display name for the hosted app")
	slug := fs.String("slug", "", "optional subdomain slug prefix when creating a new app")
	description := fs.String("description", "", "application or revision description")
	kind := fs.String("kind", "static", "app kind: static or dynamic")
	spa := fs.Bool("spa", false, "enable SPA index.html fallback for extensionless routes")
	visibility := fs.String("visibility", "", "visibility mode: private, selected, or organization (defaults to private for new apps)")
	var userIDs repeatableFlag
	var teamIDs repeatableFlag
	fs.Var(&userIDs, "user", "authorized user ID when --visibility=selected (repeatable)")
	fs.Var(&teamIDs, "team", "authorized team/department ID when --visibility=selected (repeatable)")
	reason := fs.String("reason", "", "optional reason when requesting visibility update (<= 500 chars)")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries (1..128 chars)")
	dryRun := fs.Bool("dry-run", false, "scan local bundle; with --app-id also run server preflight, without uploading")
	yes := fs.Bool("yes", false, "confirm publishing without interactive prompt")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return hostDeployOptions{}, err
	}
	if len(positionals) > 1 {
		return hostDeployOptions{}, usageError{message: "host deploy accepts at most one [dir] path"}
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return hostDeployOptions{}, usageError{message: err.Error()}
	}
	opts := hostDeployOptions{
		dir:              resolveHostDeployDir(positionals),
		appID:            strings.TrimSpace(*appID),
		name:             strings.TrimSpace(*name),
		nameExplicit:     hostFlagWasSet(fs, "name"),
		slug:             strings.TrimSpace(*slug),
		description:      strings.TrimSpace(*description),
		descExplicit:     hostFlagWasSet(fs, "description"),
		kind:             sohaapi.HostedAppKind(strings.ToLower(strings.TrimSpace(*kind))),
		spa:              *spa,
		spaExplicit:      hostFlagWasSet(fs, "spa"),
		visibilityReason: strings.TrimSpace(*reason),
		idempotencyKey:   strings.TrimSpace(*idempotencyKey),
		dryRun:           *dryRun,
		yes:              *yes,
		profile:          *profile,
		format:           format,
	}
	if err := validateHostDeployMetadata(&opts, hostFlagWasSet(fs, "visibility"), *visibility, userIDs, teamIDs); err != nil {
		return hostDeployOptions{}, err
	}
	return opts, nil
}

func resolveHostDeployDir(positionals []string) string {
	if len(positionals) == 1 && strings.TrimSpace(positionals[0]) != "" {
		return strings.TrimSpace(positionals[0])
	}
	return "."
}

func validateHostDeployMetadata(opts *hostDeployOptions, visibilitySet bool, rawVisibility string, userIDs, teamIDs []string) error {
	if !opts.kind.Valid() {
		return usageError{message: "--kind must be static or dynamic"}
	}
	if opts.kind == sohaapi.HostedAppKindDynamic && opts.spaExplicit {
		return usageError{message: "--spa is not supported for dynamic apps"}
	}
	if opts.slug != "" {
		if opts.appID != "" {
			return usageError{message: "--slug can only be set when creating a new app; subdomain labels are immutable"}
		}
		if len(opts.slug) > 48 || !hostSlugPattern.MatchString(opts.slug) {
			return usageError{message: "--slug must be 1..48 lowercase alphanumeric or hyphen characters"}
		}
	}
	if len(opts.idempotencyKey) > 128 {
		return usageError{message: "--idempotency-key must be at most 128 characters"}
	}
	if opts.nameExplicit && (opts.name == "" || len(opts.name) > 128) {
		return usageError{message: "--name must be 1..128 non-whitespace characters"}
	}
	if len(opts.description) > 512 {
		return usageError{message: "--description must be at most 512 characters"}
	}
	if len(opts.visibilityReason) > 500 {
		return usageError{message: "--reason must be at most 500 characters"}
	}
	if visibilitySet || len(userIDs) > 0 || len(teamIDs) > 0 {
		if strings.TrimSpace(rawVisibility) == "" {
			return usageError{message: "--user and --team require --visibility selected"}
		}
		parsedVis, visErr := buildHostVisibilityInput(rawVisibility, userIDs, teamIDs)
		if visErr != nil {
			return visErr
		}
		opts.visibilityInput = &parsedVis
	} else if opts.visibilityReason != "" {
		return usageError{message: "--reason requires --visibility"}
	}
	return nil
}

func runHostDeploy(ctx context.Context, args []string, rt Runtime) error {
	opts, err := parseHostDeployOptions(args, rt)
	if err != nil {
		return err
	}
	var bundle hostStaticBundle
	if opts.kind == sohaapi.HostedAppKindDynamic {
		bundle, err = buildHostDynamicBundle(opts.dir)
	} else {
		bundle, err = buildHostStaticBundle(opts.dir)
	}
	if err != nil {
		return err
	}
	if opts.dryRun && opts.appID == "" {
		return writeHostLocalDryRunOutput(rt.Out, opts.format, bundle)
	}
	if !opts.dryRun && !opts.yes {
		targetLabel := opts.appID
		if targetLabel == "" {
			targetLabel = firstNonEmptyString(opts.name, inferDefaultHostedAppName(bundle.AbsDir))
		}
		kindLabel := "static bundle"
		if opts.kind == sohaapi.HostedAppKindDynamic {
			kindLabel = "dynamic source"
		}
		confirmed, confirmErr := confirmAction(rt, fmt.Sprintf("Deploy %s %s (%d files, %d bytes) to %s (%s)?", kindLabel, terminalLabel(opts.dir), bundle.FileCount, bundle.UncompressedBytes, terminalLabel(targetLabel), opts.kind))
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return fmt.Errorf("host deploy declined; pass --yes for non-interactive use")
		}
	}

	client, err := loadHostClient(ctx, rt, opts.profile)
	if err != nil {
		return err
	}
	app, spaFallback, err := resolveDeployTargetApp(ctx, client, opts, bundle)
	if err != nil {
		return err
	}

	compressed := bundle.CompressedBytes
	preflightRequest := sohaapi.HostedAppPreflightRequest{
		Kind:              opts.kind,
		CompressedBytes:   &compressed,
		UncompressedBytes: bundle.UncompressedBytes,
		FileCount:         bundle.FileCount,
		HasIndexHTML:      bundle.HasIndexHTML,
		SamplePaths:       bundle.SamplePaths,
	}
	if opts.kind == sohaapi.HostedAppKindStatic {
		preflightRequest.SpaFallback = &spaFallback
	}
	preflight, err := client.PreflightDeployment(ctx, app.ID, preflightRequest)
	if err != nil {
		return err
	}
	if !preflight.Allowed || opts.dryRun {
		if writeErr := writeHostPreflightOutput(rt.Out, opts.format, bundle, preflight); writeErr != nil {
			return writeErr
		}
		if !preflight.Allowed {
			return fmt.Errorf("deployment preflight blocked (%d blocker(s))", maxInt(len(preflight.Blockers), 1))
		}
		return nil
	}

	app, err = applyExistingAppDeployUpdates(ctx, client, opts, app)
	if err != nil {
		return err
	}

	uploadKey := opts.idempotencyKey
	if uploadKey == "" {
		uploadKey = defaultUploadIdempotencyKey(app, bundle.BundleSHA256, spaFallback, opts.description)
	}
	uploadParams := sohaapi.UploadHostedAppRevisionParams{
		IdempotencyKey: uploadKey,
		Description:    opts.description,
	}
	if opts.kind == sohaapi.HostedAppKindStatic {
		uploadParams.SpaFallback = &spaFallback
	}
	receipt, err := client.UploadRevision(ctx, app.ID, bytes.NewReader(bundle.Archive), uploadParams)
	if err != nil {
		return err
	}
	note := ""
	if opts.visibilityInput != nil && opts.visibilityInput.Mode == sohaapi.HostedAppVisibilityModeOrganization {
		note = firstNonEmptyString(formatPendingApprovalSubmissionNote(receipt.App), formatPendingApprovalSubmissionNote(app))
	}
	if err := writeHostDeploymentReceiptOutput(rt.Out, opts.format, receipt, uploadKey, note); err != nil {
		return err
	}
	if opts.kind == sohaapi.HostedAppKindDynamic && receipt.Revision.Status == sohaapi.HostedAppRevisionStatusFailed {
		return fmt.Errorf("dynamic revision failed; after correcting the cause, retry with --app-id %s and a new --idempotency-key", terminalLabel(receipt.App.ID))
	}
	return nil
}

func resolveDeployTargetApp(ctx context.Context, client *hostClient, opts hostDeployOptions, bundle hostStaticBundle) (sohaapi.HostedApp, bool, error) {
	if opts.appID != "" {
		app, err := client.GetApp(ctx, opts.appID)
		if err != nil {
			return sohaapi.HostedApp{}, false, err
		}
		if app.Kind != opts.kind {
			return sohaapi.HostedApp{}, false, fmt.Errorf("hosted app %s has kind %q, not %q; refusing deployment before any mutation", app.ID, app.Kind, opts.kind)
		}
		spaFallback := app.SpaFallback
		if opts.kind == sohaapi.HostedAppKindDynamic {
			spaFallback = false
		}
		if opts.spaExplicit {
			spaFallback = opts.spa
		}
		return app, spaFallback, nil
	}

	appName := opts.name
	if appName == "" {
		appName = inferDefaultHostedAppName(bundle.AbsDir)
	}
	vis := sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate}
	if opts.visibilityInput != nil {
		vis = *opts.visibilityInput
	}
	spaFallback := opts.spa
	createKey := opts.idempotencyKey
	if createKey == "" {
		createKey = defaultCreateAppIdempotencyKey(opts.kind, bundle.AbsDir, appName, opts.slug, vis)
	}
	createRequest := sohaapi.CreateHostedAppRequest{
		Name:           appName,
		Slug:           opts.slug,
		Description:    opts.description,
		Kind:           opts.kind,
		Visibility:     &vis,
		IdempotencyKey: createKey,
	}
	if opts.kind == sohaapi.HostedAppKindStatic {
		createRequest.SpaFallback = &spaFallback
	}
	app, err := client.CreateApp(ctx, createRequest)
	if err != nil {
		return sohaapi.HostedApp{}, false, err
	}
	return app, spaFallback, nil
}

func applyExistingAppDeployUpdates(ctx context.Context, client *hostClient, opts hostDeployOptions, app sohaapi.HostedApp) (sohaapi.HostedApp, error) {
	if opts.appID == "" {
		return app, nil
	}
	if opts.nameExplicit || opts.descExplicit || opts.spaExplicit {
		var patch sohaapi.UpdateHostedAppRequest
		if opts.nameExplicit {
			nameCopy := opts.name
			patch.Name = &nameCopy
		}
		if opts.descExplicit {
			descCopy := opts.description
			patch.Description = &descCopy
		}
		if opts.spaExplicit {
			spaCopy := opts.spa
			patch.SpaFallback = &spaCopy
		}
		updated, err := client.UpdateApp(ctx, app.ID, patch)
		if err != nil {
			return sohaapi.HostedApp{}, err
		}
		app = updated
	}
	if opts.visibilityInput != nil {
		updated, err := client.UpdateVisibility(ctx, app.ID, sohaapi.UpdateHostedAppVisibilityRequest{
			Visibility: *opts.visibilityInput,
			Reason:     opts.visibilityReason,
		})
		if err != nil {
			return sohaapi.HostedApp{}, err
		}
		app = updated
	}
	return app, nil
}

func runHostList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host list", args, rt)
	scope := fs.String("scope", "mine", "list scope: mine or all")
	query := fs.String("query", "", "search by name, description, or subdomain")
	status := fs.String("status", "", "filter by status: draft, online, offline, or suspended_by_admin")
	limit := fs.Int("limit", 50, "page size (1..200)")
	offset := fs.Int("offset", 0, "pagination offset (>= 0)")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) != 0 {
		return usageError{message: "host list does not accept positional arguments"}
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}
	scopeVal := sohaapi.HostedAppListScope(strings.TrimSpace(*scope))
	if scopeVal != "" && !scopeVal.Valid() {
		return usageError{message: "--scope must be mine or all"}
	}
	statusVal := sohaapi.HostedAppStatus(strings.TrimSpace(*status))
	if statusVal != "" && !statusVal.Valid() {
		return usageError{message: "--status must be draft, online, offline, or suspended_by_admin"}
	}
	trimmedQuery := strings.TrimSpace(*query)
	if len(trimmedQuery) > 200 {
		return usageError{message: "--query must be at most 200 characters"}
	}
	if *limit < 1 || *limit > 200 {
		return usageError{message: "--limit must be between 1 and 200"}
	}
	if *offset < 0 {
		return usageError{message: "--offset must be greater than or equal to 0"}
	}

	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	list, err := client.ListApps(ctx, sohaapi.ListHostedAppsParams{
		Scope:  scopeVal,
		Query:  trimmedQuery,
		Status: statusVal,
		Limit:  *limit,
		Offset: *offset,
	})
	if err != nil {
		return err
	}
	return writeHostedAppListOutput(rt.Out, format, list)
}

func runHostGet(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host get", args, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "get")
	if err != nil {
		return err
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}
	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	app, err := client.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	return writeHostedAppDetailOutput(rt.Out, format, app, "")
}

func runHostRevisions(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host revisions", args, rt)
	revisionID := fs.String("revision", "", "optional revision ID to inspect a single revision")
	limit := fs.Int("limit", 50, "page size (1..200)")
	offset := fs.Int("offset", 0, "pagination offset (>= 0)")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "revisions")
	if err != nil {
		return err
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}
	if *limit < 1 || *limit > 200 {
		return usageError{message: "--limit must be between 1 and 200"}
	}
	if *offset < 0 {
		return usageError{message: "--offset must be greater than or equal to 0"}
	}

	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	if trimmedRev := strings.TrimSpace(*revisionID); trimmedRev != "" {
		if hostFlagWasSet(fs, "limit") || hostFlagWasSet(fs, "offset") {
			return usageError{message: "--limit and --offset cannot be combined with --revision"}
		}
		rev, revErr := client.GetRevision(ctx, appID, trimmedRev)
		if revErr != nil {
			return revErr
		}
		return writeHostedAppRevisionDetailOutput(rt.Out, format, rev)
	}
	list, err := client.ListRevisions(ctx, appID, sohaapi.ListHostedAppRevisionsParams{
		Limit:  *limit,
		Offset: *offset,
	})
	if err != nil {
		return err
	}
	return writeHostedAppRevisionListOutput(rt.Out, format, list)
}

func runHostRollback(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host rollback", args, rt)
	revisionID := fs.String("revision", "", "target ready revision ID")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries (1..128 chars)")
	reason := fs.String("reason", "", "rollback reason")
	yes := fs.Bool("yes", false, "confirm rollback without interactive prompt")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "rollback")
	if err != nil {
		return err
	}
	targetRevision := strings.TrimSpace(*revisionID)
	if targetRevision == "" {
		return usageError{message: "host rollback requires --revision <revision-id>"}
	}
	trimmedKey := strings.TrimSpace(*idempotencyKey)
	if len(trimmedKey) > 128 {
		return usageError{message: "--idempotency-key must be at most 128 characters"}
	}
	trimmedReason := strings.TrimSpace(*reason)
	if len(trimmedReason) > 512 {
		return usageError{message: "--reason must be at most 512 characters"}
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}

	if !*yes {
		confirmed, confirmErr := confirmAction(rt, fmt.Sprintf("Roll back hosted app %s to revision %s?", terminalLabel(appID), terminalLabel(targetRevision)))
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return fmt.Errorf("host rollback declined; pass --yes for non-interactive use")
		}
	}
	if trimmedKey == "" {
		trimmedKey = hashIdempotencyKey("host-rbk-", appID, targetRevision, trimmedReason)
	}
	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	receipt, err := client.Rollback(ctx, appID, sohaapi.HostedAppRollbackRequest{
		TargetRevisionID: targetRevision,
		IdempotencyKey:   trimmedKey,
		Reason:           trimmedReason,
	})
	if err != nil {
		return err
	}
	return writeHostDeploymentReceiptOutput(rt.Out, format, receipt, trimmedKey, "")
}

func runHostVisibility(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host visibility", args, rt)
	mode := fs.String("mode", "", "visibility mode: private, selected, or organization")
	var userIDs repeatableFlag
	var teamIDs repeatableFlag
	fs.Var(&userIDs, "user", "authorized user ID when --mode=selected (repeatable)")
	fs.Var(&teamIDs, "team", "authorized team/department ID when --mode=selected (repeatable)")
	reason := fs.String("reason", "", "optional reason for organization approval request or withdrawal")
	withdraw := fs.Bool("withdraw", false, "withdraw pending organization visibility approval request")
	yes := fs.Bool("yes", false, "confirm visibility change without interactive prompt")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "visibility")
	if err != nil {
		return err
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}
	trimmedReason := strings.TrimSpace(*reason)

	if *withdraw {
		if hostFlagWasSet(fs, "mode") || len(userIDs) > 0 || len(teamIDs) > 0 {
			return usageError{message: "--withdraw cannot be combined with --mode, --user, or --team"}
		}
		return executeHostVisibilityWithdraw(ctx, rt, appID, *profile, format, trimmedReason)
	}
	if strings.TrimSpace(*mode) == "" {
		return usageError{message: "host visibility requires --mode <private|selected|organization> or --withdraw"}
	}
	visInput, err := buildHostVisibilityInput(*mode, userIDs, teamIDs)
	if err != nil {
		return err
	}
	if len(trimmedReason) > 500 {
		return usageError{message: "--reason must be at most 500 characters"}
	}
	if !*yes {
		prompt := fmt.Sprintf("Update visibility of hosted app %s to %s?", terminalLabel(appID), terminalLabel(string(visInput.Mode)))
		if visInput.Mode == sohaapi.HostedAppVisibilityModePrivate || visInput.Mode == sohaapi.HostedAppVisibilityModeSelected {
			prompt = fmt.Sprintf("Update visibility of hosted app %s to %s (removed colleagues lose access immediately)?", terminalLabel(appID), terminalLabel(string(visInput.Mode)))
		}
		confirmed, confirmErr := confirmAction(rt, prompt)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return fmt.Errorf("host visibility update declined; pass --yes for non-interactive use")
		}
	}

	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	app, err := client.UpdateVisibility(ctx, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: visInput,
		Reason:     trimmedReason,
	})
	if err != nil {
		return err
	}
	return writeHostedAppDetailOutput(rt.Out, format, app, resolveVisibilityUpdateNote(visInput.Mode, app))
}

func executeHostVisibilityWithdraw(ctx context.Context, rt Runtime, appID, profile, format, trimmedReason string) error {
	if len(trimmedReason) > 512 {
		return usageError{message: "--reason must be at most 512 characters"}
	}
	client, err := loadHostClient(ctx, rt, profile)
	if err != nil {
		return err
	}
	var req *sohaapi.HostedAppLifecycleActionRequest
	if trimmedReason != "" {
		req = &sohaapi.HostedAppLifecycleActionRequest{Reason: trimmedReason}
	}
	app, err := client.WithdrawVisibilityApproval(ctx, appID, req)
	if err != nil {
		return err
	}
	note := fmt.Sprintf("pending organization visibility approval withdrawn; effective visibility remains %s", safeHostField(string(app.Visibility.Mode)))
	return writeHostedAppDetailOutput(rt.Out, format, app, note)
}

func resolveVisibilityUpdateNote(requestedMode sohaapi.HostedAppVisibilityMode, app sohaapi.HostedApp) string {
	if pendingNote := formatPendingApprovalSubmissionNote(app); pendingNote != "" {
		return pendingNote
	}
	switch requestedMode {
	case sohaapi.HostedAppVisibilityModePrivate:
		return "visibility is now private; saved links and active colleague sessions are immediately denied"
	case sohaapi.HostedAppVisibilityModeSelected:
		return "route generation advanced; access is immediately restricted to the selected users and teams"
	case sohaapi.HostedAppVisibilityModeOrganization:
		if app.Visibility.Mode == sohaapi.HostedAppVisibilityModeOrganization {
			return "visibility is now organization; all authenticated workspace members can access this app"
		}
	}
	return ""
}

func runHostMaintainers(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host maintainers", args, rt)
	var rawUsers repeatableFlag
	fs.Var(&rawUsers, "users", "comma-separated collaborator user IDs to set as maintainers")
	fs.Var(&rawUsers, "user", "collaborator user ID to include as maintainer (repeatable)")
	clear := fs.Bool("clear", false, "remove all collaborator maintainers")
	_ = fs.Bool("yes", false, "compatibility flag for non-interactive maintainer updates")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "maintainers")
	if err != nil {
		return err
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}
	usersSet := hostFlagWasSet(fs, "users") || hostFlagWasSet(fs, "user")
	if *clear && usersSet {
		return usageError{message: "--clear cannot be combined with --users"}
	}
	if !*clear && !usersSet {
		client, clientErr := loadHostClient(ctx, rt, *profile)
		if clientErr != nil {
			return clientErr
		}
		app, getErr := client.GetApp(ctx, appID)
		if getErr != nil {
			return getErr
		}
		return writeHostMaintainersOutput(rt.Out, format, app)
	}

	maintainerIDs := []string{}
	if usersSet {
		parsedIDs, parseErr := parseMaintainerUserIDs(rawUsers)
		if parseErr != nil {
			return parseErr
		}
		maintainerIDs = parsedIDs
	}
	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	app, err := client.UpdateMaintainers(ctx, appID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: maintainerIDs,
	})
	if err != nil {
		return err
	}
	return writeHostMaintainersOutput(rt.Out, format, app)
}

func parseMaintainerUserIDs(rawValues []string) ([]string, error) {
	seen := make(map[string]struct{}, len(rawValues))
	out := make([]string, 0, len(rawValues))
	for _, raw := range rawValues {
		for _, part := range strings.Split(raw, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				return nil, usageError{message: "--users requires non-empty user IDs; pass --clear to remove all collaborators"}
			}
			if len(id) > 128 || strings.ContainsAny(id, " \t\r\n/\\") {
				return nil, usageError{message: "--users requires valid user IDs of at most 128 characters"}
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, usageError{message: "--users requires at least one user ID; pass --clear to remove all collaborators"}
	}
	if len(out) > 20 {
		return nil, usageError{message: "--users supports at most 20 collaborator user IDs"}
	}
	return out, nil
}

func runHostDelete(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host delete", args, rt)
	confirm := fs.String("confirm", "", "exact hosted application display name required to confirm permanent deletion")
	reason := fs.String("reason", "", "optional deletion reason for audit log (<= 500 chars)")
	_ = fs.Bool("yes", false, "compatibility flag; permanent deletion always requires --confirm <exact-app-name>")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "delete")
	if err != nil {
		return err
	}
	confirmName := strings.TrimSpace(*confirm)
	if confirmName == "" {
		return usageError{message: fmt.Sprintf("host delete requires --confirm <exact-app-name> matching the exact application name for secondary confirmation（需传入精确应用名称二次确认，例如: soha host delete %s --confirm \"My App\"）", appID)}
	}
	if len(confirmName) > 128 {
		return usageError{message: "--confirm application name must be 1..128 characters"}
	}
	trimmedReason := strings.TrimSpace(*reason)
	if len(trimmedReason) > 500 {
		return usageError{message: "--reason must be at most 500 characters"}
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}

	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	result, err := client.DeleteApp(ctx, appID, sohaapi.DeleteHostedAppRequest{
		ConfirmationName: confirmName,
		Reason:           trimmedReason,
	})
	if err != nil {
		return err
	}
	return writeHostDeleteOutput(rt.Out, format, result, appID, confirmName)
}

func runHostLifecycle(ctx context.Context, action string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host "+action, args, rt)
	reason := fs.String("reason", "", "optional audit reason")
	yes := fs.Bool("yes", false, "confirm lifecycle change without interactive prompt")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, action)
	if err != nil {
		return err
	}
	trimmedReason := strings.TrimSpace(*reason)
	if len(trimmedReason) > 512 {
		return usageError{message: "--reason must be at most 512 characters"}
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}

	if !*yes {
		verb := "Take hosted app " + terminalLabel(appID) + " offline?"
		if action == "online" {
			verb = "Bring hosted app " + terminalLabel(appID) + " online?"
		}
		confirmed, confirmErr := confirmAction(rt, verb)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return fmt.Errorf("host %s declined; pass --yes for non-interactive use", action)
		}
	}

	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	var req *sohaapi.HostedAppLifecycleActionRequest
	if trimmedReason != "" {
		req = &sohaapi.HostedAppLifecycleActionRequest{Reason: trimmedReason}
	}
	var app sohaapi.HostedApp
	if action == "offline" {
		app, err = client.TakeOffline(ctx, appID, req)
	} else {
		app, err = client.BringOnline(ctx, appID, req)
	}
	if err != nil {
		return err
	}
	return writeHostedAppDetailOutput(rt.Out, format, app, "")
}

func runHostAudience(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host audience", args, rt)
	query := fs.String("query", "", "search colleagues or departments by name, username, or slug")
	limit := fs.Int("limit", 20, "maximum candidates per category (1..100)")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")

	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) != 0 {
		return usageError{message: "host audience does not accept positional arguments; pass --query <text>"}
	}
	trimmedQuery := strings.TrimSpace(*query)
	if len(trimmedQuery) > 200 {
		return usageError{message: "--query must be at most 200 characters"}
	}
	if *limit < 1 || *limit > 100 {
		return usageError{message: "--limit must be between 1 and 100"}
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}

	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	candidates, err := client.ListAudienceCandidates(ctx, sohaapi.ListAppHostingAudienceCandidatesParams{
		Query: trimmedQuery,
		Limit: *limit,
	})
	if err != nil {
		return err
	}
	return writeHostAudienceOutput(rt.Out, format, candidates)
}

func buildHostVisibilityInput(rawMode string, rawUsers, rawTeams []string) (sohaapi.HostedAppVisibilityInput, error) {
	mode := sohaapi.HostedAppVisibilityMode(strings.ToLower(strings.TrimSpace(rawMode)))
	if !mode.Valid() {
		return sohaapi.HostedAppVisibilityInput{}, usageError{message: "visibility mode must be private, selected, or organization"}
	}
	users, err := normalizeAudienceIDs(rawUsers, "--user")
	if err != nil {
		return sohaapi.HostedAppVisibilityInput{}, err
	}
	teams, err := normalizeAudienceIDs(rawTeams, "--team")
	if err != nil {
		return sohaapi.HostedAppVisibilityInput{}, err
	}
	if mode == sohaapi.HostedAppVisibilityModeSelected {
		if len(users) == 0 && len(teams) == 0 {
			return sohaapi.HostedAppVisibilityInput{}, usageError{message: "selected visibility requires at least one --user or --team"}
		}
		return sohaapi.HostedAppVisibilityInput{Mode: mode, UserIDs: users, TeamIDs: teams}, nil
	}
	if len(users) > 0 || len(teams) > 0 {
		return sohaapi.HostedAppVisibilityInput{}, usageError{message: fmt.Sprintf("--user and --team are only valid when visibility mode is selected (got %s)", mode)}
	}
	return sohaapi.HostedAppVisibilityInput{Mode: mode}, nil
}

func normalizeAudienceIDs(values []string, flagName string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		item := strings.TrimSpace(raw)
		if item == "" || len(item) > 128 {
			return nil, usageError{message: fmt.Sprintf("%s requires a non-empty ID of at most 128 characters", flagName)}
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out, nil
}

func singleHostAppID(positionals []string, action string) (string, error) {
	if len(positionals) != 1 {
		return "", usageError{message: fmt.Sprintf("host %s requires exactly one <app-id>", action)}
	}
	appID := strings.TrimSpace(positionals[0])
	if appID == "" || len(appID) > 128 || strings.ContainsAny(appID, "/\\ \t\r\n") {
		return "", usageError{message: fmt.Sprintf("host %s requires a valid <app-id>", action)}
	}
	return appID, nil
}

func parseHostInterspersedFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	flagArgs := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positionals = append(positionals, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flagArgs = append(flagArgs, arg)
		if strings.Contains(arg, "=") || isHelpArg(arg) {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		lookedUp := fs.Lookup(name)
		if lookedUp == nil {
			continue
		}
		if boolFlag, ok := lookedUp.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue
		}
		if index+1 < len(args) {
			index++
			flagArgs = append(flagArgs, args[index])
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	positionals = append(positionals, fs.Args()...)
	return positionals, nil
}

func hostFlagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func defaultCreateAppIdempotencyKey(kind sohaapi.HostedAppKind, absDir, name, slug string, vis sohaapi.HostedAppVisibilityInput) string {
	if kind == sohaapi.HostedAppKindDynamic {
		return hashIdempotencyKey(
			"host-app-dyn-",
			string(kind),
			absDir,
			name,
			slug,
			string(vis.Mode),
			strings.Join(vis.UserIDs, ","),
			strings.Join(vis.TeamIDs, ","),
		)
	}
	return hashIdempotencyKey(
		"host-app-",
		absDir,
		name,
		slug,
		string(vis.Mode),
		strings.Join(vis.UserIDs, ","),
		strings.Join(vis.TeamIDs, ","),
	)
}

func defaultUploadIdempotencyKey(app sohaapi.HostedApp, bundleSHA256 string, spaFallback bool, description string) string {
	return hashIdempotencyKey(
		"host-rev-",
		app.ID,
		app.ActiveRevisionID,
		strconv.FormatInt(app.RouteGeneration, 10),
		bundleSHA256,
		strconv.FormatBool(spaFallback),
		description,
	)
}

func hashIdempotencyKey(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:16])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
