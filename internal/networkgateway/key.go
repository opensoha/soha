package networkgateway

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/opensoha/soha/internal/platform/securefile"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func LoadOrCreatePrivateKey(path string) (wgtypes.Key, error) {
	if !filepath.IsAbs(path) {
		return wgtypes.Key{}, fmt.Errorf("WireGuard private key path must be absolute")
	}
	key, err := loadPrivateKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return wgtypes.Key{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return wgtypes.Key{}, fmt.Errorf("create WireGuard state directory: %w", err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("open WireGuard state directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	directory, err := root.Stat(".")
	if err != nil || !directory.IsDir() || directory.Mode().Perm()&0o022 != 0 {
		return wgtypes.Key{}, fmt.Errorf("WireGuard state directory must not be group- or world-writable")
	}
	key, err = wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("generate WireGuard private key: %w", err)
	}
	name := filepath.Base(path)
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		raw, readErr := securefile.ReadRoot(root, name, 128, true)
		return parsePrivateKey(raw, readErr)
	}
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("create WireGuard private key: %w", err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = root.Remove(name)
		}
	}()
	if _, err := io.WriteString(file, key.String()+"\n"); err != nil {
		return wgtypes.Key{}, fmt.Errorf("write WireGuard private key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return wgtypes.Key{}, fmt.Errorf("sync WireGuard private key: %w", err)
	}
	if err := file.Close(); err != nil {
		return wgtypes.Key{}, fmt.Errorf("close WireGuard private key: %w", err)
	}
	remove = false
	return key, nil
}

func loadPrivateKey(path string) (wgtypes.Key, error) {
	raw, err := securefile.Read(path, 128, true)
	return parsePrivateKey(raw, err)
}

func parsePrivateKey(raw []byte, err error) (wgtypes.Key, error) {
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("read bounded WireGuard private key: %w", err)
	}
	key, err := wgtypes.ParseKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return wgtypes.Key{}, fmt.Errorf("parse WireGuard private key: %w", err)
	}
	return key, nil
}
