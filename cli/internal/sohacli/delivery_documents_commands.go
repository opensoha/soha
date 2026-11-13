package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	deliverydoc "github.com/opensoha/soha-contracts/delivery"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runDeliveryDocuments(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("delivery documents requires validate, preview, import, export, or source")
	}
	action, args := args[0], args[1:]
	if action == "source" {
		return runDeliveryDocumentSource(ctx, args, rt)
	}
	if action == "export" {
		return runDeliveryDocumentExport(ctx, args, rt)
	}
	if action == "import" {
		return runDeliveryDocumentImport(ctx, args, rt)
	}
	if action != "validate" && action != "preview" {
		return fmt.Errorf("unknown delivery documents action %q", action)
	}
	fs := newRuntimeFlagSet("delivery documents "+action, args, rt)
	profile := fs.String("profile", "", "profile name (preview only)")
	output := fs.String("output", "json", "result format: json or yaml")
	inputFile := fs.String("input", "", "preview request JSON file, or - for stdin; supports explicit per-file targetId and expectedRevision")
	files := repeatableFlag{}
	fs.Var(&files, "file", "delivery YAML/JSON file, repeatable; - reads stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("use --file or --input for delivery documents")
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	input, err := readDeliveryDocumentInput(rt, files, *inputFile)
	if err != nil {
		return err
	}
	if action == "validate" {
		return validateDeliveryDocuments(rt, input, format)
	}
	_, _, selected, err := loadRuntimeProfile(ctx, rt, *profile)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, selected)
	var result sohaapi.DeliveryDocumentPreviewEnvelope
	if err := client.doJSON(ctx, http.MethodPost, "/api/v1/delivery/documents/preview", client.Token, nil, input, &result); err != nil {
		return err
	}
	if err := writeStructuredOutput(rt.Out, format, result); err != nil {
		return err
	}
	if !result.Data.Valid {
		return fmt.Errorf("document preview failed; review diagnostics before importing")
	}
	return nil
}

func readDeliveryDocumentInput(rt Runtime, files []string, inputPath string) (sohaapi.DeliveryDocumentPreviewInput, error) {
	var input sohaapi.DeliveryDocumentPreviewInput
	if (len(files) == 0) == (inputPath == "") {
		return input, fmt.Errorf("provide either --file (repeatable) or --input")
	}
	if inputPath != "" {
		// JSON escaping can expand each source byte sixfold.
		raw, err := readBoundedDeliveryFile(rt, inputPath, 6*deliverydoc.MaxTotalBytes+65536)
		if err != nil {
			return input, err
		}
		if !utf8.Valid(raw) {
			return input, fmt.Errorf("preview request JSON must be UTF-8")
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return input, fmt.Errorf("invalid preview request JSON")
		}
		if decoder.Decode(new(any)) != io.EOF || input.ValidateOnly {
			return input, fmt.Errorf("provide one preview request without validateOnly; use validate for offline checks")
		}
	} else {
		if len(files) > deliverydoc.MaxFiles {
			return input, fmt.Errorf("at most %d files are allowed", deliverydoc.MaxFiles)
		}
		total := 0
		for _, file := range files {
			raw, err := readBoundedDeliveryFile(rt, file, deliverydoc.MaxFileBytes)
			if err != nil {
				return input, err
			}
			total += len(raw)
			if total > deliverydoc.MaxTotalBytes || !utf8.Valid(raw) {
				return input, fmt.Errorf("documents must be UTF-8 and total at most 2 MiB")
			}
			name := filepath.Base(file)
			if file == "-" {
				name = "stdin.yaml"
			}
			input.Files = append(input.Files, sohaapi.DeliveryDocumentFile{Path: name, Content: string(raw)})
		}
	}
	if len(input.Files) == 0 || len(input.Files) > deliverydoc.MaxFiles {
		return input, fmt.Errorf("provide 1 to %d files", deliverydoc.MaxFiles)
	}
	seen, total := map[string]bool{}, 0
	for _, file := range input.Files {
		if !deliverydoc.ValidPath(file.Path) || seen[file.Path] {
			return input, fmt.Errorf("file display paths must be unique normalized relative paths; use --input to name files in different directories")
		}
		seen[file.Path] = true
		total += len(file.Content)
		if len(file.Content) > deliverydoc.MaxFileBytes || total > deliverydoc.MaxTotalBytes {
			return input, fmt.Errorf("documents exceed the 1 MiB per-file or 2 MiB total limit")
		}
		if (file.TargetID != "") != (file.ExpectedRevision > 0) || file.ExpectedRevision < 0 {
			return input, fmt.Errorf("an update requires both targetId and a positive expectedRevision")
		}
	}
	return input, nil
}

func readBoundedDeliveryFile(rt Runtime, name string, limit int64) ([]byte, error) {
	reader := rt.In
	if name != "-" {
		file, err := os.Open(name) // #nosec G304 -- the CLI user explicitly selects this local input file.
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("delivery input must be a regular file")
		}
		reader = file
	}
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("delivery input exceeds its byte limit")
	}
	return raw, nil
}

func validateDeliveryDocuments(rt Runtime, input sohaapi.DeliveryDocumentPreviewInput, format string) error {
	result := struct {
		Valid       bool                     `json:"valid"`
		Scope       string                   `json:"scope"`
		Documents   []deliverydoc.Document   `json:"documents"`
		Diagnostics []deliverydoc.Diagnostic `json:"diagnostics"`
	}{true, "offline syntax and schema; preview checks permissions, references and revisions", []deliverydoc.Document{}, []deliverydoc.Diagnostic{}}
	for _, file := range input.Files {
		document, diagnostics := deliverydoc.Parse(file.Path, []byte(file.Content))
		if len(diagnostics) > 0 {
			result.Valid = false
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
		} else {
			result.Documents = append(result.Documents, document)
		}
	}
	if err := writeStructuredOutput(rt.Out, format, result); err != nil {
		return err
	}
	if !result.Valid {
		return fmt.Errorf("document validation failed; see diagnostics")
	}
	return nil
}

func runDeliveryDocumentImport(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("delivery documents import", args, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "result format: json or yaml")
	previewID := fs.String("preview-id", "", "valid preview ID; reuse when retrying an uncertain response")
	digest := fs.String("candidate-digest", "", "exact candidateDigest from preview")
	key := fs.String("idempotency-key", "", "stable attempt key; reuse with the same preview on retry")
	yes := fs.Bool("yes", false, "confirm importing the preview into drafts; authorization still applies")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !validDeliveryDocumentID(*previewID) || strings.TrimSpace(*digest) == "" || strings.TrimSpace(*key) == "" {
		return fmt.Errorf("import requires --preview-id, --candidate-digest and --idempotency-key from a reviewed preview")
	}
	if len(*key) < 8 || len(*key) > 128 || !strings.HasPrefix(*digest, "sha256:") || !sha256Pattern.MatchString(strings.TrimPrefix(*digest, "sha256:")) {
		return fmt.Errorf("import requires an 8–128 byte idempotency key and the sha256 candidate digest from preview")
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	if !*yes {
		confirmed, err := confirmAction(rt, "Import the reviewed delivery preview into drafts?")
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("import declined; pass --yes after reviewing the preview for non-interactive use")
		}
	}
	_, _, selected, err := loadRuntimeProfile(ctx, rt, *profile)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, selected)
	input := sohaapi.DeliveryDocumentApplyInput{CandidateDigest: *digest, IdempotencyKey: *key}
	var result sohaapi.DeliveryDocumentImportEnvelope
	if err := client.doJSON(ctx, http.MethodPost, "/api/v1/delivery/documents/imports/"+url.PathEscape(*previewID)+"/apply", client.Token, nil, input, &result); err != nil {
		return fmt.Errorf("import failed; retry with the same preview ID, candidate digest and idempotency key: %w", err)
	}
	return writeStructuredOutput(rt.Out, format, result)
}

func runDeliveryDocumentExport(ctx context.Context, args []string, rt Runtime) error {
	leading, args := extractLeadingPositionals(args, 2)
	fs := newRuntimeFlagSet("delivery documents export", args, rt)
	profile := fs.String("profile", "", "profile name")
	format := fs.String("format", "yaml", "document representation: yaml or json")
	version := fs.Int("version", 0, "required published template version; omit only for Workflow")
	destination := fs.String("out", "", "new output file; omit to write the document to stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 2 || !validDeliveryDocumentID(positionals[1]) {
		return fmt.Errorf("export requires a document kind and object ID")
	}
	switch positionals[0] {
	case "BuildTemplate", "WorkflowTemplate", "DeploymentTemplate", "Workflow":
	default:
		return fmt.Errorf("unsupported delivery document kind")
	}
	if (positionals[0] != "Workflow" && *version < 1) || (positionals[0] == "Workflow" && *version != 0) {
		return fmt.Errorf("templates require a positive --version; Workflow exports its current saved definition without --version")
	}
	representation, err := normalizeOutputFormat(*format, "yaml", "json")
	if err != nil {
		return err
	}
	_, _, selected, err := loadRuntimeProfile(ctx, rt, *profile)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, selected)
	query := url.Values{"format": {representation}}
	if *version != 0 {
		query.Set("version", strconv.Itoa(*version))
	}
	path := "/api/v1/delivery/documents/" + positionals[0] + "/" + url.PathEscape(positionals[1]) + "/export?" + query.Encode()
	var result sohaapi.DeliveryDocumentExportEnvelope
	if err := client.doJSON(ctx, http.MethodGet, path, client.Token, nil, nil, &result); err != nil {
		return err
	}
	if *destination == "" {
		_, err = io.WriteString(rt.Out, result.Data.Content)
		return err
	}
	file, err := os.OpenFile(*destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, result.Data.Content)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func validDeliveryDocumentID(value string) bool {
	return strings.TrimSpace(value) != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}
