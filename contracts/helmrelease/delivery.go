package helmrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const (
	MaxChartArchiveBytes  = 5 << 20
	MaxChartExpandedBytes = 20 << 20
	MaxChartFiles         = 1000
)

var (
	helmName            = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	helmVersion         = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	helmSHA256          = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	helmSecretReference = regexp.MustCompile(`^soha://secrets/[A-Za-z0-9._-]+(?:/versions/[1-9][0-9]*)?$`)
)

// ValidateSource rejects executable/local sources and ambiguous, mutable chart references.
// Credential authorization and resolution belong to the consuming application.
func ValidateSource(source sohaapi.DeploymentTemplateHelmSource) error {
	u, err := url.Parse(source.RepositoryURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "oci") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("helm repository must be an HTTPS or OCI URL without embedded credentials, query or fragment")
	}
	if strings.Contains(u.Path, "\\") || path.Clean("/"+u.Path) != strings.TrimSuffix("/"+strings.TrimPrefix(u.Path, "/"), "/") && u.Path != "" && u.Path != "/" {
		return errors.New("helm repository path must be canonical")
	}
	if !helmName.MatchString(source.Chart) || len(source.Chart) > 253 || !helmVersion.MatchString(source.Version) {
		return errors.New("helm chart requires a chart name and an exact semantic version")
	}
	if source.Digest != "" && !helmSHA256.MatchString(source.Digest) {
		return errors.New("helm chart digest must be a SHA-256 digest")
	}
	if source.ConnectionID != "" {
		return errors.New("helm chart connections are unavailable; use scoped secret references")
	}
	if source.SecretRefs != nil {
		for alias, reference := range *source.SecretRefs {
			if alias != "CHART_USERNAME" && alias != "CHART_PASSWORD" && alias != "CHART_CA_CERT" {
				return errors.New("unsupported Helm chart credential reference")
			}
			if !helmSecretReference.MatchString(reference) {
				return errors.New("helm chart credentials require Soha secret references")
			}
		}
	}
	return nil
}

func ValidateConfiguration(config sohaapi.HelmDeliveryConfiguration) error {
	if err := ValidateSource(config.Source); err != nil {
		return err
	}
	if !helmName.MatchString(config.ReleaseName) || len(config.ReleaseName) > 53 {
		return errors.New("helm release name must be a DNS label of at most 53 characters")
	}
	if config.TimeoutSeconds < 0 || config.TimeoutSeconds > 3600 {
		return errors.New("helm timeout must be between 1 and 3600 seconds")
	}
	seen := map[string]bool{}
	for _, mapping := range config.ImageMappings {
		if mapping.ContainerName == "" || !mapping.Value.Valid() {
			return errors.New("helm image mapping requires a container and an immutable image field")
		}
		if _, err := pointerParts(mapping.Path); err != nil {
			return err
		}
		if seen[mapping.Path] {
			return errors.New("helm image mapping paths must be unique")
		}
		seen[mapping.Path] = true
	}
	return nil
}

func SHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ValidateChartArchive bounds both compressed and expanded input before an SDK loads it.
// Symlinks, duplicate entries and dependency downloads are never followed here.
func ValidateChartArchive(content []byte) error {
	budget, files := int64(MaxChartExpandedBytes), 0
	return validateChartArchive(content, &budget, &files, 0)
}

func validateChartArchive(content []byte, budget *int64, files *int, depth int) error {
	if len(content) == 0 || len(content) > MaxChartArchiveBytes {
		return errors.New("helm chart archive exceeds the 5 MiB limit or is empty")
	}
	if depth > 8 {
		return errors.New("helm chart dependencies exceed the nesting limit")
	}
	zr, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return errors.New("helm chart must be a gzip archive")
	}
	defer func() { _ = zr.Close() }()
	bounded := chartBudgetReader{reader: zr, remaining: budget}
	reader := tar.NewReader(bounded)
	seen := map[string]bool{}
	root := ""
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || *budget <= 0 || *files >= MaxChartFiles {
			return errors.New("helm chart archive is invalid or exceeds its file/expanded size limit")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if err := validateChartEntry(header, name, seen); err != nil {
			return err
		}
		folder, _, _ := strings.Cut(name, "/")
		if root != "" && root != folder {
			return errors.New("helm chart archive must contain one root directory")
		}
		root, seen[name] = folder, true
		*files++
		if strings.Contains(name, "/crds/") {
			return errors.New("application Helm charts cannot install cluster-scoped CRDs; install them through platform management first")
		}
		if header.Typeflag == tar.TypeReg && path.Base(name) == "values.schema.json" {
			content, err := io.ReadAll(reader)
			if err != nil {
				return errors.New("read Helm values schema")
			}
			if err := validateValuesSchema(content); err != nil {
				return err
			}
		}
		if header.Typeflag == tar.TypeReg && strings.Contains(name, "/charts/") && strings.HasSuffix(name, ".tgz") {
			child, err := io.ReadAll(io.LimitReader(reader, MaxChartArchiveBytes+1))
			if err != nil {
				return errors.New("read Helm chart dependency")
			}
			if err := validateChartArchive(child, budget, files, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := io.Copy(io.Discard, bounded); err != nil || *budget <= 0 || !seen[root+"/Chart.yaml"] {
		return errors.New("helm chart archive is invalid, oversized or missing Chart.yaml")
	}
	return nil
}

func validateValuesSchema(content []byte) error {
	var schema any
	if err := json.Unmarshal(content, &schema); err != nil {
		return errors.New("helm values schema must be valid JSON")
	}
	return validateSchemaReferences(schema)
}

func validateSchemaReferences(value any) error {
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			if reference, ok := value.(string); ok {
				switch key {
				case "$ref", "$dynamicRef", "$recursiveRef":
					if reference != "" && !strings.HasPrefix(reference, "#") {
						return errors.New("helm values schemas require references within the same document")
					}
				case "$schema":
					if !knownSchemaDialect(reference) {
						return errors.New("helm values schema uses an unsupported schema dialect")
					}
				}
			}
			if err := validateSchemaReferences(value); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range item {
			if err := validateSchemaReferences(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func knownSchemaDialect(value string) bool {
	value = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://"), "#")
	switch value {
	case "json-schema.org/draft-04/schema", "json-schema.org/draft-06/schema", "json-schema.org/draft-07/schema", "json-schema.org/draft/2019-09/schema", "json-schema.org/draft/2020-12/schema":
		return true
	default:
		return false
	}
}

func validateChartEntry(header *tar.Header, name string, seen map[string]bool) error {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || seen[name] {
		return errors.New("helm chart archive contains an unsafe or duplicate path")
	}
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir || header.Size < 0 || header.Size > MaxChartExpandedBytes {
		return errors.New("helm chart archive contains an unsupported file type or size")
	}
	return nil
}

type chartBudgetReader struct {
	reader    io.Reader
	remaining *int64
}

func (r chartBudgetReader) Read(p []byte) (int, error) {
	if *r.remaining <= 0 {
		return 0, errors.New("helm chart exceeds the expanded size limit")
	}
	if int64(len(p)) > *r.remaining {
		p = p[:*r.remaining]
	}
	n, err := r.reader.Read(p)
	*r.remaining -= int64(n)
	return n, err
}

// ValidateTaskIdentity bounds the confidential RPC/claim input before native SDK work.
// Prepare accepts an unfinished render; Execute additionally verifies its digests.
func ValidateTaskIdentity(input sohaapi.HelmExecutionTaskPayload) error {
	s := input.Snapshot
	if IsInitialization(s) && (s.Operation != sohaapi.HelmDeliverySnapshotOperationInstall || s.ExpectedRevision != 0 || s.RollbackRevision != 0 || s.Migration.Blocked || !s.Migration.RequiresApproval || s.Migration.FromVersion != 0 || s.TemplateInputs == nil || s.TemplateInputs.TemplateVersion != s.Migration.ToVersion) {
		return errors.New("initialization requires an approved create-only first deployment snapshot")
	}
	for _, value := range []string{s.DeliveryPlanID, s.TargetID, s.ApplicationID, s.ApplicationEnvironmentID, s.ServiceID, s.ClusterID} {
		if strings.TrimSpace(value) == "" || len(value) > 512 {
			return errors.New("helm task identity is incomplete")
		}
	}
	if !input.Action.Valid() || !helmName.MatchString(s.Namespace) || len(s.Namespace) > 63 || !helmName.MatchString(s.ReleaseName) || len(s.ReleaseName) > 53 || s.ExpectedRevision < 0 || s.RollbackRevision < 0 || s.TimeoutSeconds < 0 || s.TimeoutSeconds > 3600 {
		return errors.New("helm task action, namespace, revision or timeout is invalid")
	}
	if len(s.Resources) > 300 || len(input.InitializationResources) > 300 {
		return errors.New("helm task exceeds 300 resources")
	}
	if input.Prepared != nil {
		prepared := input.Prepared
		if len(prepared.ChartArchive) > 6990508 || len(prepared.Manifest) > 2<<20 || len(prepared.Hooks) > 50 {
			return errors.New("helm preparation exceeds its content limits")
		}
		total := len(prepared.Manifest)
		for _, hook := range prepared.Hooks {
			total += len(hook)
		}
		encoded, err := json.Marshal(prepared.Values)
		if err != nil || total > 2<<20 || len(encoded) > 1<<20 {
			return errors.New("helm rendered content or values exceeds its limit")
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 8<<20 {
		return errors.New("helm task exceeds the RPC limit")
	}
	return nil
}

// LegacyAllowedActions restricts delivery-owned releases to inspection through
// the legacy resource API. Nil leaves unmanaged releases to normal authorization.
func LegacyAllowedActions(labels map[string]string) []string {
	if labels["soha-delivery-owner"] != "" {
		return []string{"view"}
	}
	return nil
}
