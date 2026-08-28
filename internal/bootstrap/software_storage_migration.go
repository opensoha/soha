package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func rejectLegacySoftwareStorage(configuredPath string) error {
	paths := []string{"data/software"}
	if configuredPath = strings.TrimSpace(configuredPath); configuredPath != "" {
		paths = append(paths, configuredPath)
	}
	if configured := strings.TrimSpace(os.Getenv("SOHA_SOFTWARE_STORAGE_DIR")); configured != "" {
		paths = append(paths, configured)
	}
	return checkLegacySoftwareStorage(paths)
}

func checkLegacySoftwareStorage(paths []string) error {
	seen := map[string]struct{}{}
	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		indexPath := filepath.Join(root, "index.json")
		if _, ok := seen[indexPath]; ok {
			continue
		}
		seen[indexPath] = struct{}{}
		if _, err := os.Stat(indexPath); err == nil {
			return fmt.Errorf("legacy software packages detected at %s; export and re-upload them to S3-compatible storage before upgrading", indexPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect legacy software package index %s: %w", indexPath, err)
		}
	}
	return nil
}
