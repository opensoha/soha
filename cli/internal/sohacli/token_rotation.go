package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type localRotationRequest struct {
	RequestID       string    `json:"requestId"`
	Server          string    `json:"server"`
	Kind            string    `json:"kind"`
	PreviousTokenID string    `json:"previousTokenId"`
	AuthProfile     string    `json:"authProfile"`
	ExpiresAt       time.Time `json:"expiresAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

func keyExpiry(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Now().UTC().Add(90 * 24 * time.Hour), nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil || !parsed.After(time.Now()) {
		return time.Time{}, usageError{message: "--expires-at must be a future RFC3339 timestamp"}
	}
	return parsed.UTC(), nil
}
func rotationClient(ctx context.Context, rt Runtime, requested, authProfile, target string) (APIClient, string, error) {
	var p ProfileConfig
	var name string
	var err error
	if authProfile == "" {
		_, name, p, err = loadRuntimeProfile(ctx, rt, requested)
	} else {
		_, metadata, e := statusMetadata(rt, requested)
		if e != nil {
			return APIClient{}, "", e
		}
		cfg, e := loadConfig(rt.ConfigPath)
		if e != nil {
			return APIClient{}, "", e
		}
		selected, ok := cfg.Profiles[profileName(authProfile)]
		if !ok {
			return APIClient{}, "", fmt.Errorf("--auth-profile is not configured")
		}
		if !sameServerURL(metadata.ServerURL, selected.ServerURL) {
			return APIClient{}, "", fmt.Errorf("server_mismatch: --auth-profile must belong to the same server as the selected target profile")
		}
		_, name, p, err = loadRuntimeProfileWithToken(ctx, rt, authProfile, selected.ServerURL, "")
	}
	if err != nil {
		return APIClient{}, "", err
	}
	client := gatewayClient(rt, p)
	identity, err := client.AuthContext(ctx)
	if err != nil {
		return APIClient{}, "", authRequestFailure(err)
	}
	if err = validateAuthContext(identity); err != nil {
		return APIClient{}, "", err
	}
	if target != "" && (identity.CredentialKind == "personal_access_token" || identity.CredentialKind == "service_account_token") && identity.TokenID == target {
		return APIClient{}, "", fmt.Errorf("independent_auth_required: select a different valid session or management Key with --auth-profile; the Key being rotated cannot recover its own lost response")
	}
	return client, name, nil
}
func runTokenRotate(ctx context.Context, args []string, rt Runtime, kind string) error {
	fs := newRuntimeFlagSet("token rotate", args, rt)
	profile := fs.String("profile", "", "target profile/server")
	authProfile := fs.String("auth-profile", "", "independent authentication profile on the same server; ignores SOHA_TOKEN")
	tokenID := fs.String("id", "", "previous Key ID")
	requestID := fs.String("request-id", "", "UUID for recovering a previous rotation attempt")
	expires := fs.String("expires-at", "", "future RFC3339 timestamp; defaults to 90 days")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := firstNonEmptyString(*tokenID, fs.Arg(0))
	if id == "" {
		return usageError{message: "rotation requires a Key ID"}
	}
	expiry, err := keyExpiry(*expires)
	if err != nil {
		return err
	}
	request := uuid.New()
	if *requestID != "" {
		request, err = uuid.Parse(*requestID)
		if err != nil || len(*requestID) != 36 {
			return usageError{message: "--request-id must be a UUID"}
		}
	}
	client, name, err := rotationClient(ctx, rt, *profile, *authProfile, id)
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(rt.ConfigPath), "token-rotations", request.String()+".json")
	if err = rejectConfigSymlink(path); err != nil {
		return err
	}
	if raw, e := os.ReadFile(path); e == nil { // #nosec G304 -- Parsed UUID filename under the private config root; symlinks rejected above.
		var previous localRotationRequest
		if len(raw) > 8192 || json.Unmarshal(raw, &previous) != nil || previous.RequestID != request.String() || previous.Kind != kind || previous.PreviousTokenID != id || !sameServerURL(previous.Server, client.ServerURL) {
			return fmt.Errorf("rotation_request_conflict: saved request belongs to another Key or server")
		}
		expiry = previous.ExpiresAt
	} else if !os.IsNotExist(e) {
		return e
	} else {
		record := localRotationRequest{RequestID: request.String(), Server: client.ServerURL, Kind: kind, PreviousTokenID: id, AuthProfile: name, ExpiresAt: expiry, CreatedAt: time.Now().UTC()}
		_, statErr := os.Lstat(filepath.Dir(path))
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err = requirePrivateConfigDirectory(filepath.Dir(path), os.IsNotExist(statErr)); err != nil {
			return err
		}
		raw, e = json.Marshal(record)
		if e != nil {
			return e
		}
		if err = writeAtomicFile(path, append(raw, '\n'), 0o600); err != nil {
			return err
		}
	}
	if _, err = fmt.Fprintf(rt.Err, "Rotation request: %s. The old Key is revoked on success. Use rotation-status with this ID and an independent credential if the response is lost.\n", request); err != nil {
		return err
	}
	input := sohaapi.TokenRotationInput{RotationRequestID: request, ExpiresAt: &expiry}
	result, err := client.RotateGatewayCredential(ctx, kind, id, input)
	if err == nil {
		return writePrettyJSON(rt.Out, map[string]any{"rotationRequestId": request, "result": sanitizeCreatedToken(result)})
	}
	return authRequestFailure(err)
}
func runTokenRotationStatus(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("token rotation-status", args, rt)
	profile := fs.String("profile", "", "target profile/server")
	authProfile := fs.String("auth-profile", "", "independent authentication profile")
	id := fs.String("request-id", "", "rotation request UUID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsed, err := uuid.Parse(*id)
	if err != nil || len(*id) != 36 {
		return usageError{message: "--request-id must be a UUID"}
	}
	client, _, err := rotationClient(ctx, rt, *profile, *authProfile, "")
	if err != nil {
		return err
	}
	var out sohaapi.TokenRotationRecordEnvelope
	if err = client.doJSON(ctx, http.MethodGet, "/api/v1/ai-gateway/token-rotations/"+parsed.String(), client.Token, nil, nil, &out); err != nil {
		return authRequestFailure(err)
	}
	return writePrettyJSON(rt.Out, out.Data)
}
