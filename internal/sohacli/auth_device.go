package sohacli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type pendingLoginReference struct {
	Ref      string `json:"ref"`
	Store    string `json:"store"`
	ConfigID string `json:"configId"`
}

func pendingLoginFile(rt Runtime, server, profile string) (string, error) {
	configID, err := credentialConfigScope(rt.ConfigPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(rt.ConfigPath), "pending-logins", configID+"-"+credentialBinding(server, profile)+".json"), nil
}
func savePendingLogin(rt Runtime, store credentialStorage, o loginOptions, pending pendingDeviceLogin) (func() error, error) {
	if store.mode == "ephemeral" {
		return func() error { return nil }, nil
	}
	ref := newCredentialRef(o.server, o.profile)
	path, err := pendingLoginFile(rt, o.server, o.profile)
	if err != nil {
		return nil, err
	}
	configID, err := credentialConfigScope(rt.ConfigPath)
	if err != nil || configID != store.configID {
		return nil, fmt.Errorf("pending login storage does not belong to this configuration")
	}
	envelope := credentialEnvelope{Version: 2, Server: o.server, Profile: o.profile, Kind: "pending_login", PendingLogin: &pending, ConfigID: configID}
	if err := store.Save(ref, envelope); err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	_, statErr := os.Lstat(dir)
	err = os.MkdirAll(dir, 0o700)
	if err == nil {
		err = requirePrivateConfigDirectory(dir, os.IsNotExist(statErr))
	}
	raw, _ := json.Marshal(pendingLoginReference{Ref: ref, Store: store.mode, ConfigID: configID})
	if err == nil {
		err = rejectConfigSymlink(path)
	}
	if err == nil {
		err = writeAtomicFile(path, append(raw, '\n'), 0o600)
	}
	if err != nil {
		_ = store.Delete(ref)
		return nil, err
	}
	return pendingLoginCleanup(path, store, ref), nil
}
func pendingLoginCleanup(path string, store credentialStorage, ref string) func() error {
	return func() error {
		if err := rejectConfigSymlink(path); err != nil {
			return err
		}
		raw, e := os.ReadFile(path) // #nosec G304 -- Profile-bound reference under the private config root; symlinks rejected above.
		if os.IsNotExist(e) {
			return store.Delete(ref)
		}
		if e != nil {
			return e
		}
		var current pendingLoginReference
		if json.Unmarshal(raw, &current) != nil || current.ConfigID != store.configID {
			return fmt.Errorf("invalid pending login reference")
		}
		if current.Ref != ref {
			return store.Delete(ref)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return store.Delete(ref)
	}
}
func loadPendingLogin(rt Runtime, o loginOptions) (*pendingDeviceLogin, credentialStorage, func() error, error) {
	var empty credentialStorage
	path, err := pendingLoginFile(rt, o.server, o.profile)
	if err != nil {
		return nil, empty, nil, err
	}
	if err := rejectConfigSymlink(path); err != nil {
		return nil, empty, nil, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- Profile-bound reference under the private config root; symlinks rejected above.
	if os.IsNotExist(err) {
		return nil, empty, nil, nil
	}
	if err != nil {
		return nil, empty, nil, err
	}
	if len(raw) > 4096 {
		return nil, empty, nil, fmt.Errorf("invalid pending login reference")
	}
	var ref pendingLoginReference
	configID, scopeErr := credentialConfigScope(rt.ConfigPath)
	if json.Unmarshal(raw, &ref) != nil || scopeErr != nil || ref.ConfigID != configID {
		return nil, empty, nil, fmt.Errorf("invalid pending login reference")
	}
	store, err := openCredentialStorage(rt, ref.Store)
	if err != nil {
		return nil, empty, nil, err
	}
	envelope, err := store.Load(ref.Ref, o.server, o.profile)
	if err != nil {
		return nil, empty, nil, err
	}
	if envelope.Kind != "pending_login" || envelope.PendingLogin == nil {
		return nil, empty, nil, fmt.Errorf("invalid pending login credential")
	}
	p := envelope.PendingLogin
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(p.UserCode) || len(p.CodeVerifier) < 43 || len(p.CodeVerifier) > 128 || p.DeviceCode == "" || p.Interval < 1 || p.Interval > 30 {
		return nil, empty, nil, fmt.Errorf("invalid pending login state")
	}
	if _, err = uuid.Parse(p.ClaimRequestID); err != nil {
		return nil, empty, nil, fmt.Errorf("invalid pending login request ID")
	}
	return envelope.PendingLogin, store, pendingLoginCleanup(path, store, ref.Ref), nil
}
func loginWithDevice(ctx context.Context, rt Runtime, client APIClient, store *credentialStorage, o loginOptions) (loginResponse, func() error, error) {
	var empty loginResponse
	// One login flow per target profile. Ordinary credential refresh uses another
	// lock and continues while the human completes browser authentication.
	unlock, err := acquireCredentialLock(ctx, rt.ConfigPath, "pending:"+credentialBinding(o.server, o.profile))
	if err != nil {
		return empty, nil, err
	}
	defer unlock()
	pending, existingStore, cleanup, err := loadPendingLogin(rt, o)
	if err != nil {
		return empty, nil, err
	}
	if pending != nil {
		if o.restart {
			if err = cancelDeviceLogin(rt, client, *pending); err != nil {
				if pending.ExpiresAt.After(time.Now()) {
					return empty, nil, fmt.Errorf("pending_cancel_unknown: cancellation could not be confirmed; retry before starting another login")
				}
				_, _ = fmt.Fprintln(rt.Err, "The expired request is being discarded locally. Any previously issued session could not be confirmed revoked; review your CLI sessions in the Web profile.")
			}
			if err = cleanup(); err != nil {
				return empty, nil, err
			}
			pending = nil
		} else {
			*store = existingStore
			o.store = store.mode
			if !pending.ExpiresAt.After(time.Now()) {
				return empty, nil, fmt.Errorf("pending_login_expired: use --restart to cancel the previous request, then review your Web CLI sessions")
			}
		}
	}
	if err = store.Probe(o.server, o.profile); err != nil {
		return empty, nil, err
	}
	options, err := cliAuthOptionsFor(ctx, client, "device_code")
	if err != nil {
		return empty, nil, err
	}
	if pending == nil {
		verifier, challenge, e := newDeviceVerifier()
		if e != nil {
			return empty, nil, e
		}
		grant, e := client.CreateDeviceAuthorization(ctx, sohaapi.CLIDeviceAuthorizationCreateRequest{ClientID: "soha-cli", ClientVersion: version, DeviceName: o.deviceName, CodeChallenge: challenge, CodeChallengeMethod: "S256"})
		if e != nil {
			return empty, nil, e
		}
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(grant.UserCode) || grant.DeviceCode == "" || grant.Interval < 1 || grant.Interval > 30 || !grant.ExpiresAt.After(time.Now()) || grant.ExpiresAt.After(time.Now().Add(10*time.Minute)) {
			if regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(grant.DeviceCode) {
				invalid := pendingDeviceLogin{DeviceCode: grant.DeviceCode, CodeVerifier: verifier}
				if cancelErr := cancelDeviceLogin(rt, client, invalid); cancelErr != nil {
					_, _ = fmt.Fprintln(rt.Err, "Invalid authorization response; cancellation could not be confirmed. Review CLI sessions in the Web profile.")
				}
			}
			return empty, nil, fmt.Errorf("invalid_device_authorization: invalid server grant bounds")
		}
		pending = &pendingDeviceLogin{DeviceCode: grant.DeviceCode, CodeVerifier: verifier, ClaimRequestID: uuid.NewString(), VerificationURI: grant.VerificationURI, UserCode: grant.UserCode, ExpiresAt: grant.ExpiresAt, Interval: int(grant.Interval)}
		if e = validateVerificationURL(pending.VerificationURI, options.VerificationOrigin, o.allowHTTP); e != nil {
			_ = cancelDeviceLogin(rt, client, *pending)
			return empty, nil, e
		}
		cleanup, e = savePendingLogin(rt, *store, o, *pending)
		if e != nil {
			_ = cancelDeviceLogin(rt, client, *pending)
			return empty, nil, e
		}
	}
	if err = validateVerificationURL(pending.VerificationURI, options.VerificationOrigin, o.allowHTTP); err != nil {
		return empty, nil, err
	}
	human := rt.Out
	if o.json {
		human = rt.Err
	}
	if _, err = fmt.Fprintf(human, "Instance: %s\nOpen: %s\nVerification code: %s %s\nExpires: %s\n", o.server, pending.VerificationURI, pending.UserCode[:3], pending.UserCode[3:], pending.ExpiresAt.Local().Format(time.RFC3339)); err != nil {
		return empty, nil, err
	}
	if !o.noBrowser {
		if e := openDeviceBrowser(ctx, rt, pending.VerificationURI); e != nil {
			_, _ = fmt.Fprintln(rt.Err, "The browser could not be opened. Copy the displayed link to continue.")
		}
	}
	_, _ = fmt.Fprintln(rt.Err, "Waiting for browser confirmation; Ctrl+C cancels this request.")
	result, err := pollDeviceLogin(ctx, client, *pending)
	if errors.Is(err, context.Canceled) {
		if cancelErr := cancelDeviceLogin(rt, client, *pending); cancelErr != nil {
			_, _ = fmt.Fprintln(rt.Err, "Cancellation could not be confirmed. Review your CLI sessions in the Web profile; the pending login was retained for recovery.")
		} else if cleanup != nil {
			_ = cleanup()
		}
	}
	return result, cleanup, err
}
func pollDeviceLogin(ctx context.Context, client APIClient, pending pendingDeviceLogin) (loginResponse, error) {
	var empty loginResponse
	id, err := uuid.Parse(pending.ClaimRequestID)
	if err != nil {
		return empty, fmt.Errorf("invalid pending claim request")
	}
	ctx, cancel := context.WithDeadline(ctx, pending.ExpiresAt)
	defer cancel()
	input := sohaapi.CLIDeviceTokenRequest{DeviceCode: pending.DeviceCode, CodeVerifier: pending.CodeVerifier, ClaimRequestID: id}
	interval := time.Duration(pending.Interval) * time.Second
	backoff := interval
	for {
		result, err := client.ExchangeDeviceToken(ctx, input)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		var status *httpStatusError
		if errors.As(err, &status) {
			switch {
			case status.Code == "authorization_pending":
				backoff = interval
			case status.Code == "slow_down":
				interval = min(interval+5*time.Second, 30*time.Second)
				backoff = interval
			case status.StatusCode == http.StatusTooManyRequests:
				backoff = min(max(backoff, interval), 30*time.Second)
			case status.StatusCode >= 500:
				backoff = min(max(backoff*2, interval), 30*time.Second)
			default:
				return empty, authRequestFailure(err)
			}
			if status.RetryAfter > 0 {
				backoff = max(backoff, time.Duration(min(status.RetryAfter, 600))*time.Second)
			}
		} else {
			backoff = min(max(backoff*2, interval), 30*time.Second)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return empty, ctx.Err()
		case <-timer.C:
		}
	}
}
func cancelDeviceLogin(rt Runtime, client APIClient, pending pendingDeviceLogin) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.CancelDeviceAuthorization(ctx, sohaapi.CLIDeviceCancelRequest{DeviceCode: pending.DeviceCode, CodeVerifier: pending.CodeVerifier})
}
