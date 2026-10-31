package sohacli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func canonicalCredentialURL(value string) (*url.URL, error) {
	u, err := url.Parse(normalizeServerURL(value))
	if err != nil {
		return nil, fmt.Errorf("invalid_server_url")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("invalid_server_url")
	}
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}
func validateLoginServer(value string, allowHTTP bool) (string, error) {
	u, err := canonicalCredentialURL(value)
	if err != nil {
		return "", fmt.Errorf("invalid_server_url: use an HTTP(S) server URL without credentials, query or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if u.Scheme == "http" && !loopback && !allowHTTP {
		return "", fmt.Errorf("insecure_server: HTTPS is required; use --allow-insecure-http explicitly for a trusted development server")
	}
	return u.String(), nil
}
func lockConfig(ctx context.Context, rt Runtime) (func(), error) {
	return acquireCredentialLock(ctx, rt.ConfigPath, "config")
}
func lockProfile(ctx context.Context, rt Runtime, server, name string) (func(), error) {
	return acquireCredentialLock(ctx, rt.ConfigPath, "profile:"+canonicalServerURL(server)+":"+name)
}
func acquireCredentialLock(ctx context.Context, configPath, scope string) (func(), error) {
	configID, err := credentialConfigScope(configPath)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(configPath)
	_, statErr := os.Lstat(parent)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	if err := requirePrivateConfigDirectory(parent, os.IsNotExist(statErr)); err != nil {
		return nil, err
	}
	dir := filepath.Join(parent, "locks")
	_, statErr = os.Lstat(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := requirePrivateConfigDirectory(dir, os.IsNotExist(statErr)); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(configID + "\x00" + scope))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".lock")
	if err := rejectConfigSymlink(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- SHA256 scope filename in the verified private lock directory; symlinks rejected.
	if err != nil {
		return nil, err
	}
	if err = restrictPrivateFile(path); err != nil {
		_ = file.Close()
		return nil, err
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		locked, e := tryCredentialFileLock(file)
		if e != nil {
			_ = file.Close()
			return nil, e
		}
		if locked {
			return func() { _ = unlockCredentialFile(file); _ = file.Close() }, nil
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-deadline.Done():
			timer.Stop()
			_ = file.Close()
			return nil, &authenticationError{Code: "store_busy", Message: "Credential storage is busy; retry when the other command finishes", cause: deadline.Err()}
		case <-timer.C:
		}
	}
}
