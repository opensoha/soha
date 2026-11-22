package sohacli

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

type hostBundleContentPattern struct {
	name    string
	pattern *regexp.Regexp
}

var hostBundleContentPatterns = []hostBundleContentPattern{
	{name: "private_key", pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----|-----BEGIN PGP PRIVATE KEY BLOCK-----`)},
	{name: "aws_access_key", pattern: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{name: "credential_assignment", pattern: regexp.MustCompile(`(?i)(?:password|passwd|api[_-]?key|client[_-]?secret|access[_-]?token)\s*["']?\s*[:=]\s*["']?[^\s"',;}{]{8,}`)},
}

var hostBundleTextExtensions = map[string]struct{}{
	".html": {}, ".htm": {}, ".js": {}, ".mjs": {}, ".cjs": {},
	".css": {}, ".json": {}, ".map": {}, ".webmanifest": {}, ".svg": {},
	".txt": {}, ".xml": {}, ".md": {}, ".csv": {}, ".yaml": {}, ".yml": {},
	".toml": {}, ".py": {}, ".ts": {}, ".tsx": {}, ".jsx": {},
}

func inspectHostBundleContent(relPath string, content []byte) error {
	if len(content) == 0 || !isHostBundleTextContent(relPath) {
		return nil
	}
	matched := make([]string, 0, len(hostBundleContentPatterns))
	for _, detector := range hostBundleContentPatterns {
		if detector.pattern.Match(content) {
			matched = append(matched, detector.name)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	return fmt.Errorf("bundle file %q contains credential-like content (detected: %s); matching content is redacted from this error", relPath, strings.Join(matched, ", "))
}

func isHostBundleTextContent(relPath string) bool {
	_, ok := hostBundleTextExtensions[strings.ToLower(path.Ext(relPath))]
	return ok
}
