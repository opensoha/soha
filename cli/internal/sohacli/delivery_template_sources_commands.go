package sohacli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	deliverydoc "github.com/opensoha/soha-contracts/delivery"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func deliveryTemplateSourceEndpoint(action string) (string, string, any, any, error) {
	const base = "/api/v1/delivery/template-sources"
	switch action {
	case "list":
		return http.MethodGet, base, nil, &sohaapi.DeliveryTemplateSourceListEnvelope{}, nil
	case "get":
		return http.MethodGet, base + "/{id}", nil, &sohaapi.DeliveryTemplateSourceEnvelope{}, nil
	case "create":
		return http.MethodPost, base, &sohaapi.DeliveryTemplateSourceInput{}, &sohaapi.DeliveryTemplateSourceEnvelope{}, nil
	case "update":
		return http.MethodPut, base + "/{id}", &sohaapi.DeliveryTemplateSourceInput{}, &sohaapi.DeliveryTemplateSourceEnvelope{}, nil
	case "remove":
		return http.MethodDelete, base + "/{id}", &sohaapi.DeliveryTemplateSourceRemoveInput{}, nil, nil
	case "objects":
		return http.MethodGet, base + "/{id}/objects", nil, &sohaapi.DeliveryTemplateSourceAssociationListEnvelope{}, nil
	case "sync":
		return http.MethodPost, base + "/{id}/sync", &sohaapi.DeliveryTemplateSyncInput{}, &sohaapi.DeliveryTemplateSyncRunEnvelope{}, nil
	case "runs":
		return http.MethodGet, base + "/{id}/sync-runs", nil, &sohaapi.DeliveryTemplateSyncRunListEnvelope{}, nil
	case "run":
		return http.MethodGet, base + "/{id}/sync-runs/{runId}", nil, &sohaapi.DeliveryTemplateSyncRunEnvelope{}, nil
	case "apply":
		return http.MethodPost, base + "/{id}/sync-runs/{runId}/apply", &sohaapi.DeliveryTemplateSyncApplyInput{}, &sohaapi.DeliveryTemplateSyncRunEnvelope{}, nil
	case "detach":
		return http.MethodPost, base + "/{id}/objects/{kind}/{objectId}/detach", &sohaapi.DeliveryTemplateSourceRemoveInput{}, nil, nil
	default:
		return "", "", nil, nil, fmt.Errorf("unknown delivery template-sources action %q", action)
	}
}

type deliveryTemplateSourceFlags struct {
	runID, kind, objectID string
	offset, limit         int
}

func (f *deliveryTemplateSourceFlags) register(fs *flag.FlagSet, action string) {
	switch action {
	case "list", "objects", "runs":
		fs.IntVar(&f.offset, "offset", 0, "page offset")
		fs.IntVar(&f.limit, "limit", 50, "page size, from 1 to 200")
	case "run", "apply":
		fs.StringVar(&f.runID, "run-id", "", "sync run ID; apply consumes this exact preview without fetching again")
	case "detach":
		fs.StringVar(&f.kind, "kind", "", "delivery document kind")
		fs.StringVar(&f.objectID, "object-id", "", "associated object ID; review objects before detaching")
	}
}

func (f deliveryTemplateSourceFlags) path(action, path string) (string, error) {
	switch action {
	case "list", "objects", "runs":
		if f.offset < 0 || f.limit < 1 || f.limit > 200 {
			return "", fmt.Errorf("--offset must be nonnegative and --limit between 1 and 200")
		}
		path += "?" + url.Values{"offset": {strconv.Itoa(f.offset)}, "limit": {strconv.Itoa(f.limit)}}.Encode()
	case "run", "apply":
		if !validDeliveryDocumentID(f.runID) {
			return "", fmt.Errorf("provide --run-id from sync or runs")
		}
		path = strings.Replace(path, "{runId}", url.PathEscape(f.runID), 1)
	case "detach":
		if !sohaapi.DeliveryDocumentKind(f.kind).Valid() || !validDeliveryDocumentID(f.objectID) {
			return "", fmt.Errorf("detach requires a valid --kind and --object-id from objects")
		}
		path = strings.NewReplacer("{kind}", f.kind, "{objectId}", url.PathEscape(f.objectID)).Replace(path)
	}
	return path, nil
}

func validateDeliveryTemplateSourceInput(action string, input any) error {
	switch v := input.(type) {
	case *sohaapi.DeliveryTemplateSourceInput:
		if (action == "create" && v.ExpectedGeneration != 0) || (action == "update" && v.ExpectedGeneration < 1) {
			return fmt.Errorf("source creation requires expectedGeneration=0; update requires the loaded positive generation")
		}
		if strings.TrimSpace(v.Name) == "" || !validDeliveryDocumentID(v.RepositoryID) || strings.TrimSpace(v.RefValue) == "" || (v.Path != "." && !deliverydoc.ValidPath(v.Path)) || len(v.Kinds) == 0 {
			return fmt.Errorf("source requires name, stored repositoryId, refValue, relative path and kinds")
		}
		if v.RefType != "branch" && v.RefType != "tag" && v.RefType != "commit" {
			return fmt.Errorf("refType must be branch, tag or commit")
		}
		seen := map[sohaapi.DeliveryDocumentKind]bool{}
		for _, kind := range v.Kinds {
			if !sohaapi.DeliveryDocumentKind(kind).Valid() || seen[kind] {
				return fmt.Errorf("source kinds must be unique supported document kinds")
			}
			seen[kind] = true
		}
	case *sohaapi.DeliveryTemplateSourceRemoveInput:
		if v.ExpectedGeneration < 1 || (v.Disposition != "keep" && v.Disposition != "deprecate") {
			return fmt.Errorf("review objects first; removal requires expectedGeneration and explicit disposition keep or deprecate")
		}
	case *sohaapi.DeliveryTemplateSyncInput:
		return validateDeliveryTemplateSyncAttempt(v.ExpectedGeneration, v.IdempotencyKey)
	case *sohaapi.DeliveryTemplateSyncApplyInput:
		if !strings.HasPrefix(v.CandidateDigest, "sha256:") || !sha256Pattern.MatchString(strings.TrimPrefix(v.CandidateDigest, "sha256:")) {
			return fmt.Errorf("apply requires the exact sha256 candidateDigest from the reviewed sync preview")
		}
		return validateDeliveryTemplateSyncAttempt(v.ExpectedGeneration, v.IdempotencyKey)
	}
	return nil
}

func validateDeliveryTemplateSyncAttempt(generation int, key string) error {
	if generation < 1 || len(strings.TrimSpace(key)) < 8 || len(key) > 128 {
		return fmt.Errorf("sync/apply requires expectedGeneration and a stable 8–128 byte idempotencyKey; reuse the complete request after an uncertain response")
	}
	return nil
}

func runDeliveryDocumentSource(ctx context.Context, args []string, rt Runtime) error {
	leading, args := extractLeadingPositionals(args, 2)
	fs := newRuntimeFlagSet("delivery documents source", args, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "result format: json or yaml")
	version := fs.Int("version", 0, "optional published template version for immutable provenance")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 2 || !sohaapi.DeliveryDocumentKind(positionals[0]).Valid() || !validDeliveryDocumentID(positionals[1]) || *version < 0 || (positionals[0] == "Workflow" && *version != 0) {
		return fmt.Errorf("source requires a document kind and ID; --version is optional for templates only")
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	_, _, selected, err := loadRuntimeProfile(ctx, rt, *profile)
	if err != nil {
		return err
	}
	path := "/api/v1/delivery/documents/" + positionals[0] + "/" + url.PathEscape(positionals[1]) + "/source"
	if *version > 0 {
		path += "?version=" + strconv.Itoa(*version)
	}
	client := gatewayClient(rt, selected)
	var result sohaapi.DeliveryDocumentSourceInfoEnvelope
	if err := client.doJSON(ctx, http.MethodGet, path, client.Token, nil, nil, &result); err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, result)
}
