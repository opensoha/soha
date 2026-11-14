package sohacli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

// SecretKeyring is injectable so tests never access the user's system keychain.
type SecretKeyring interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}
type systemSecretKeyring struct{}

func (systemSecretKeyring) Set(s, u, p string) error        { return keyring.Set(s, u, p) }
func (systemSecretKeyring) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (systemSecretKeyring) Delete(s, u string) error        { return keyring.Delete(s, u) }

type credentialEnvelope struct {
	Version                  int                 `json:"version"`
	Server                   string              `json:"server"`
	Profile                  string              `json:"profile"`
	Kind                     string              `json:"kind"`
	ProtocolVersion          int                 `json:"protocolVersion,omitempty"`
	AccessToken              string              `json:"accessToken"`
	RefreshToken             string              `json:"refreshToken,omitempty"`
	ExpiresAt                time.Time           `json:"expiresAt,omitempty"`
	RefreshExpiresAt         *time.Time          `json:"refreshExpiresAt,omitempty"`
	AbsoluteExpiresAt        *time.Time          `json:"absoluteExpiresAt,omitempty"`
	SessionID                string              `json:"sessionId,omitempty"`
	LegacySessionFingerprint string              `json:"legacySessionFingerprint,omitempty"`
	Generation               int                 `json:"generation"`
	PendingRefreshRequestID  string              `json:"pendingRefreshRequestId,omitempty"`
	PendingLogin             *pendingDeviceLogin `json:"pendingLogin,omitempty"`
	ConfigID                 string              `json:"configId,omitempty"`
}
type pendingDeviceLogin struct {
	DeviceCode      string    `json:"deviceCode"`
	CodeVerifier    string    `json:"codeVerifier"`
	ClaimRequestID  string    `json:"claimRequestId"`
	VerificationURI string    `json:"verificationUri"`
	UserCode        string    `json:"userCode"`
	ExpiresAt       time.Time `json:"expiresAt"`
	Interval        int       `json:"interval"`
}

// credentialSaveUncertain means the config reference may already be active.
// Its remote session must remain valid until the caller can verify the outcome.
type credentialSaveUncertain struct{ cause error }

func (e *credentialSaveUncertain) Error() string {
	return "credential_save_unconfirmed: the configuration may have switched; verify auth status before retrying login"
}
func (e *credentialSaveUncertain) Unwrap() error { return e.cause }

type credentialStorage struct {
	mode, dir, service, configID string
	keyring                      SecretKeyring
}

var credentialRefPattern = regexp.MustCompile(`^[a-f0-9]{16}-[a-f0-9-]{36}$`)

func credentialBinding(server, profile string) string {
	sum := sha256.Sum256([]byte(canonicalServerURL(server) + "\x00" + profile))
	return hex.EncodeToString(sum[:8])
}
func newCredentialRef(server, profile string) string {
	return credentialBinding(server, profile) + "-" + uuid.NewString()
}
func canonicalServerURL(value string) string {
	// sameServerURL validates the complete credential origin including base path.
	u, err := canonicalCredentialURL(value)
	if err != nil {
		return ""
	}
	return u.String()
}

func credentialConfigScope(configPath string) (string, error) {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
	return hex.EncodeToString(sum[:12]), nil
}

func openCredentialStorage(rt Runtime, mode string) (credentialStorage, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || mode == "auto" {
		mode = "keyring"
	}
	if mode != "keyring" && mode != "file" && mode != "ephemeral" {
		return credentialStorage{}, fmt.Errorf("invalid credential store; use auto, keyring, file or ephemeral")
	}
	absolute, err := filepath.Abs(rt.ConfigPath)
	if err != nil {
		return credentialStorage{}, err
	}
	configID, err := credentialConfigScope(absolute)
	if err != nil {
		return credentialStorage{}, err
	}
	backend := rt.Keyring
	if backend == nil {
		backend = systemSecretKeyring{}
	}
	return credentialStorage{mode: mode, dir: filepath.Join(filepath.Dir(absolute), "credentials"), service: "com.opensoha.cli.v2." + configID, configID: configID, keyring: backend}, nil
}
func credentialStoreError(err error) error {
	if err == nil {
		return nil
	}
	return &authenticationError{Code: "credential_store_unavailable", Message: "Credential storage is unavailable; unlock the system keyring or explicitly choose --credential-store file", cause: err}
}
func (s credentialStorage) ensureDirectory() error {
	// Creating credentials/ can also create its configuration parent. On Windows,
	// MkdirAll's Unix mode does not secure that parent; protect it before use.
	parent := filepath.Dir(s.dir)
	_, parentErr := os.Lstat(parent)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := requirePrivateConfigDirectory(parent, os.IsNotExist(parentErr)); err != nil {
		return err
	}
	_, err := os.Lstat(s.dir)
	created := os.IsNotExist(err)
	if err = os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return requirePrivateConfigDirectory(s.dir, created)
}
func (s credentialStorage) path(ref string) (string, error) {
	if !credentialRefPattern.MatchString(ref) {
		return "", fmt.Errorf("invalid credential reference")
	}
	return filepath.Join(s.dir, ref+".json"), nil
}
func (s credentialStorage) Save(ref string, value credentialEnvelope) error {
	if _, err := s.path(ref); err != nil {
		return err
	}
	if value.Kind == "pending_login" && value.ConfigID != s.configID {
		return fmt.Errorf("pending login does not belong to this configuration")
	}
	raw, err := json.Marshal(value) // #nosec G117 -- Intentional credential envelope for the OS keyring or explicit private file; never logged or exported.
	if err != nil {
		return err
	}
	switch s.mode {
	case "keyring":
		// Bound the platform helper before it starts: macOS security's interactive
		// command limit and Windows credential blob limits are lower than file mode.
		if runtime.GOOS == "windows" && len(raw) > 2560 || runtime.GOOS == "darwin" && base64.StdEncoding.EncodedLen(len(raw))+len(s.service)+len(ref)+128 > 4096 {
			return &authenticationError{Code: "credential_store_capacity", Message: "This credential exceeds the system keyring capacity; explicitly select --credential-store file"}
		}
		return credentialStoreError(s.keyring.Set(s.service, ref, string(raw)))
	case "file":
		if err = s.ensureDirectory(); err != nil {
			return err
		}
		path, _ := s.path(ref)
		if err = rejectConfigSymlink(path); err != nil {
			return err
		}
		return writeAtomicFile(path, append(raw, '\n'), 0o600)
	case "ephemeral":
		return nil
	}
	return fmt.Errorf("invalid credential store")
}
func (s credentialStorage) Load(ref, server, profile string) (credentialEnvelope, error) {
	var value credentialEnvelope
	path, err := s.path(ref)
	if err != nil {
		return value, err
	}
	var raw []byte
	switch s.mode {
	case "keyring":
		text, e := s.keyring.Get(s.service, ref)
		if e != nil {
			return value, credentialStoreError(e)
		}
		raw = []byte(text)
	case "file":
		if err = rejectConfigSymlink(path); err != nil {
			return value, err
		}
		if err = requirePrivateConfigDirectory(s.dir, false); err != nil {
			return value, err
		}
		if err = restrictPrivateFile(path); err != nil {
			return value, err
		}
		raw, err = os.ReadFile(path) // #nosec G304 -- Bound UUID reference, private directory/file and symlink checks above.
		if err != nil {
			return value, fmt.Errorf("credential_missing: log in again or restore the selected credential store")
		}
	default:
		return value, fmt.Errorf("ephemeral credentials are available only in the current invocation")
	}
	if len(raw) > 1<<20 {
		return value, fmt.Errorf("invalid credential envelope")
	}
	if err = json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("invalid credential envelope")
	}
	if value.Version != 2 || value.Profile != profile || !sameServerURL(value.Server, server) || !strings.HasPrefix(ref, credentialBinding(server, profile)+"-") {
		return value, fmt.Errorf("server_mismatch: credential envelope does not belong to this server and profile")
	}
	if value.Kind == "pending_login" && value.ConfigID != s.configID {
		return value, fmt.Errorf("pending login does not belong to this configuration")
	}
	return value, nil
}
func (s credentialStorage) Delete(ref string) error {
	path, err := s.path(ref)
	if err != nil {
		return err
	}
	if s.mode == "keyring" {
		err = s.keyring.Delete(s.service, ref)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return credentialStoreError(err)
	}
	if s.mode == "file" {
		if err = rejectConfigSymlink(path); err != nil {
			return err
		}
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}
func (s credentialStorage) Probe(server, profile string) error {
	if s.mode == "ephemeral" {
		return nil
	}
	ref := newCredentialRef(server, profile)
	probe := credentialEnvelope{Version: 2, Server: server, Profile: profile, Kind: "probe"}
	defer func() { _ = s.Delete(ref) }()
	if err := s.Save(ref, probe); err != nil {
		return err
	}
	_, err := s.Load(ref, server, profile)
	return err
}
func hydrateProfile(rt Runtime, name string, p ProfileConfig) (ProfileConfig, error) {
	if p.CredentialRef == "" {
		return p, nil
	}
	store, err := openCredentialStorage(rt, p.CredentialStore)
	if err != nil {
		return p, err
	}
	value, err := store.Load(p.CredentialRef, p.ServerURL, name)
	if err != nil {
		return p, err
	}
	p.AccessToken = value.AccessToken
	p.RefreshToken = value.RefreshToken
	p.ExpiresAt = value.ExpiresAt
	p.ProtocolVersion = value.ProtocolVersion
	p.CredentialKind = value.Kind
	p.credential = &value
	return p, nil
}
func saveAuthenticatedProfile(ctx context.Context, rt Runtime, name string, p ProfileConfig, value credentialEnvelope, mode string, makeCurrent bool) error {
	store, err := openCredentialStorage(rt, mode)
	if err != nil {
		return err
	}
	if store.mode == "ephemeral" {
		return nil
	}
	value.Version = 2
	value.Server = canonicalServerURL(p.ServerURL)
	value.Profile = name
	ref := newCredentialRef(p.ServerURL, name)
	if err = store.Save(ref, value); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = store.Delete(ref)
		}
	}()
	unlock, err := lockConfig(ctx, rt)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	previous := cfg.Profiles[name]
	p.AccessToken = ""
	p.RefreshToken = ""
	p.CredentialRef = ref
	p.CredentialStore = store.mode
	p.CredentialKind = value.Kind
	p.ProtocolVersion = value.ProtocolVersion
	p.ExpiresAt = value.ExpiresAt
	cfg.SchemaVersion = 2
	if makeCurrent {
		cfg.CurrentProfile = name
	}
	cfg.Profiles[name] = p
	writeConfig := saveConfig
	if rt.credentialConfigWrite != nil {
		writeConfig = rt.credentialConfigWrite
	}
	if err = writeConfig(rt.ConfigPath, cfg); err != nil {
		// A rename may have succeeded even if the final directory sync failed.
		// Keep its target credential in that uncertain state rather than dangling it.
		observed, readErr := loadConfig(rt.ConfigPath)
		if readErr != nil || observed.Profiles[name].CredentialRef == ref {
			committed = true
			return &credentialSaveUncertain{cause: err}
		}
		return err
	}
	committed = true
	if previous.CredentialRef != "" && previous.CredentialRef != ref {
		old, e := openCredentialStorage(rt, previous.CredentialStore)
		if e == nil {
			if e = old.Delete(previous.CredentialRef); e != nil {
				_, _ = fmt.Fprintln(rt.Err, "Authentication saved; a previous credential entry could not be removed. Check the selected credential store.")
			}
		}
	}
	return nil
}
