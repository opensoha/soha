package contentdlp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var builtin = map[string]*regexp.Regexp{
	"private_key":           regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----|-----BEGIN PGP PRIVATE KEY BLOCK-----`),
	"aws_access_key":        regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	"credential_assignment": regexp.MustCompile(`(?i)(?:password|passwd|api[_-]?key|client[_-]?secret|access[_-]?token)\s*["']?\s*[:=]\s*["']?[^\s"',;}{]{8,}`),
}
var ruleID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

// Inspect and ValidatePolicy share the detector with other owned Core transfer
// points. Authorization, reliable events and release remain with each owner.
func Inspect(ctx context.Context, c domain.Content, content []byte) domain.Decision {
	return scan(ctx, c, string(content), int64(len(content)), false)
}
func ValidatePolicy(c domain.Content) error { return validate(c) }

func validate(c domain.Content) error {
	if err := validateReleaseConditions(c); err != nil {
		return err
	}
	if c.DetectorVersion != "" && c.DetectorVersion != domain.DetectorVersion && c.DetectorVersion != domain.ArchiveDetectorVersion || c.MinimumDataLevel != "" && DataLevelRank(c.MinimumDataLevel) < 0 {
		return apperrors.ErrInvalidArgument
	}
	if c.Detectors == nil || c.Rules == nil || c.Mode != "audit" && c.Mode != "warn" && c.Mode != "block" || len(c.Detectors) > 3 || len(c.Rules) > 16 || len(c.Detectors)+len(c.Rules) == 0 {
		return fmt.Errorf("%w: invalid DLP mode or detector count", apperrors.ErrInvalidArgument)
	}
	seen := map[string]bool{}
	for _, id := range c.Detectors {
		if builtin[id] == nil || seen[id] {
			return fmt.Errorf("%w: invalid or duplicate DLP detector", apperrors.ErrInvalidArgument)
		}
		seen[id] = true
	}
	return validateRules(c.Rules, seen)
}

func validateReleaseConditions(c domain.Content) error {
	if c.RequireStepUp && c.Mode != "warn" && c.Mode != "block" {
		return apperrors.ErrInvalidArgument
	}
	if c.NetworkAction != "" && c.NetworkAction != "none" && (c.NetworkAction != "revoke_network" || c.Mode != "block") {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func validateRules(rules []domain.Rule, seen map[string]bool) error {
	for _, r := range rules {
		if r.DataLevel != "" && DataLevelRank(r.DataLevel) < 0 {
			return apperrors.ErrInvalidArgument
		}
		if !ruleID.MatchString(r.ID) || seen[r.ID] || len(r.Expression) == 0 || len(r.Expression) > 256 {
			return fmt.Errorf("%w: invalid DLP rule", apperrors.ErrInvalidArgument)
		}
		if _, err := regexp.Compile(r.Expression); err != nil {
			return fmt.Errorf("%w: invalid RE2 DLP expression", apperrors.ErrInvalidArgument)
		}
		seen[r.ID] = true
	}
	return nil
}

func scan(ctx context.Context, c domain.Content, content string, size int64, truncated bool) domain.Decision {
	d := domain.Decision{Mode: c.Mode, DetectorVersion: detectorVersion(c), MatchedRuleIDs: []string{}, RuleSetVersion: RuleSetVersion(c), MinimumDataLevel: minimumLevel(c), DataLevel: "unknown"}
	texts, status := inspectionTexts(ctx, c, content, size, truncated)
	d.Status = status
	if d.Status == "clean" {
		d.DataLevel = "unclassified"
		for _, id := range c.Detectors {
			if matchesTexts(builtin[id], texts) {
				d.MatchedRuleIDs = append(d.MatchedRuleIDs, id)
				d.DataLevel = HigherDataLevel(d.DataLevel, builtinLevel(id))
			}
		}
		for _, r := range c.Rules {
			if ctx.Err() != nil {
				d.Status = "timeout"
				break
			}
			pattern, err := regexp.Compile(r.Expression)
			if err != nil {
				d.Status = "error"
				break
			}
			if matchesTexts(pattern, texts) {
				d.MatchedRuleIDs = append(d.MatchedRuleIDs, r.ID)
				level := r.DataLevel
				if level == "" {
					level = "confidential"
				}
				d.DataLevel = HigherDataLevel(d.DataLevel, level)
			}
		}
		if d.Status == "clean" && len(d.MatchedRuleIDs) > 0 {
			d.Status = "matched"
		}
	}
	if ctx.Err() != nil {
		d.Status = "timeout"
	}
	if d.Status != "clean" && d.Status != "matched" {
		d.DataLevel = "unknown"
	}
	d.Allowed = c.Mode == "audit" || d.Status == "clean" || d.Status == "matched" && DataLevelRank(d.DataLevel) < DataLevelRank(d.MinimumDataLevel)
	d.StepUpRequired = c.RequireStepUp && !d.Allowed && d.Status == "matched"
	return d
}

func detectorVersion(c domain.Content) string {
	if c.DetectorVersion == "" {
		return domain.DetectorVersion
	}
	return c.DetectorVersion
}

func inspectionTexts(ctx context.Context, c domain.Content, content string, size int64, truncated bool) ([]string, string) {
	if detectorVersion(c) != domain.DetectorVersion && detectorVersion(c) != domain.ArchiveDetectorVersion {
		return nil, "error"
	}
	status := contentStatus(content, size, truncated)
	if c.DetectorVersion == domain.ArchiveDetectorVersion && status == "unsupported" {
		return archiveTexts(ctx, []byte(content))
	}
	return []string{content}, status
}

func matchesTexts(pattern *regexp.Regexp, texts []string) bool {
	for _, text := range texts {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func validateDockerPolicy(c domain.Content) error {
	// The Docker runtime returns strings; binary extraction requires the managed
	// transfer's original-byte contract rather than a lossy JSON string.
	if detectorVersion(c) != domain.DetectorVersion {
		return apperrors.ErrInvalidArgument
	}
	return validate(c)
}

func contentStatus(content string, size int64, truncated bool) string {
	if truncated || size > domain.MaxTextBytes || len(content) > domain.MaxTextBytes {
		return "too_large"
	}
	if size < 0 || int64(len(content)) != size {
		return "error"
	}
	if strings.Contains(content, "-----BEGIN PGP MESSAGE-----") {
		return "encrypted"
	}
	if strings.HasPrefix(content, "%PDF-") || strings.HasPrefix(content, `{\rtf`) {
		return "unsupported"
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, utf8.RuneError) {
		return "unsupported"
	}
	for _, r := range content {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' || r == 127 {
			return "unsupported"
		}
	}
	return "clean"
}
