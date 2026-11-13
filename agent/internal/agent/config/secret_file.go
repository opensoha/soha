package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxBearerTokenFileSize = 4 << 10

func resolveBearerTokenFiles(cfg *Config) error {
	if err := resolveBearerTokenFile(
		"auth",
		&cfg.Auth.BearerToken,
		&cfg.Auth.BearerTokenFile,
	); err != nil {
		return err
	}
	if err := resolveBearerTokenFile(
		"control_plane",
		&cfg.ControlPlane.BearerToken,
		&cfg.ControlPlane.BearerTokenFile,
	); err != nil {
		return err
	}
	for id, provider := range cfg.ControlPlane.AgentRuntime.Providers {
		if err := resolveBearerTokenFile("control_plane.agent_runtime.providers."+id, &provider.BearerToken, &provider.BearerTokenFile); err != nil {
			return err
		}
		cfg.ControlPlane.AgentRuntime.Providers[id] = provider
	}
	return nil
}

func resolveBearerTokenFile(section string, token, tokenFile *string) error {
	path := strings.TrimSpace(os.ExpandEnv(*tokenFile))
	*tokenFile = path
	if path == "" {
		return nil
	}
	if strings.TrimSpace(*token) != "" {
		if !isUnsafeToken(*token) {
			return fmt.Errorf("%s.bearer_token and %s.bearer_token_file are mutually exclusive", section, section)
		}
		*token = ""
	}
	resolved, err := readBearerTokenFile(path)
	if err != nil {
		return fmt.Errorf("read %s.bearer_token_file: %w", section, err)
	}
	*token = resolved
	return nil
}

func readBearerTokenFile(path string) (string, error) {
	data, err := ReadPrivateFile(path, maxBearerTokenFileSize)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("bearer token file is empty")
	}
	return token, nil
}

// ReadPrivateFile reuses the token-file boundary for guest keys, configuration and durable state.
func ReadPrivateFile(path string, maxBytes int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("private file path must be absolute")
	}
	if err := validateSecretDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}

	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect private file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("private file must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private file permissions %04o are too broad", info.Mode().Perm())
	}
	if maxBytes < 1 || info.Size() > maxBytes {
		return nil, errors.New("private file exceeds size limit")
	}

	file, err := openSecretFileNoFollow(path)
	if err != nil {
		return nil, fmt.Errorf("open private file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened private file: %w", err)
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("private file changed while it was being opened")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read private file: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("private file exceeds size limit")
	}
	return data, nil
}

func validateSecretDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect bearer token directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("bearer token directory must be a directory, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("bearer token directory permissions %04o are too broad", info.Mode().Perm())
	}
	return nil
}
