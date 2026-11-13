package sohacli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"golang.org/x/term"
)

func runAuthMFA(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return usageError{message: "auth mfa requires status, totp or verify"}
	}
	action := args[0]
	fs := newRuntimeFlagSet("auth mfa "+action, args[1:], rt)
	profile := fs.String("profile", "", "profile name")
	show := fs.Bool("show-enrollment", false, "explicitly display the one-time sensitive TOTP provisioning URI")
	id := fs.String("challenge-id", "", "original MFA challenge UUID")
	stdin := fs.Bool("response-stdin", false, "read the verification response from stdin")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageError{message: "MFA does not accept positional codes or arguments"}
	}
	allowed := []string{"profile"}
	switch action {
	case "status":
	case "totp":
		allowed = append(allowed, "show-enrollment")
	case "verify":
		allowed = append(allowed, "challenge-id", "response-stdin")
	default:
		return usageError{message: "auth mfa supports status, totp and verify"}
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return err
	}
	var response string
	if action == "verify" {
		parsed, err := uuid.Parse(*id)
		if err != nil || parsed == uuid.Nil || parsed.String() != *id || !*stdin {
			return usageError{message: "verify requires the original --challenge-id UUID and --response-stdin"}
		}
		response, err = readMFAInput(rt)
		if err != nil {
			return err
		}
	}
	_, client, err := computeOutputClient(ctx, rt, *profile, "json")
	if err != nil {
		return err
	}
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	switch action {
	case "status":
		var out sohaapi.MFACredentialListEnvelope
		if err := client.doJSON(ctx, http.MethodGet, "/api/v1/identity/mfa/credentials", client.Token, nil, nil, &out); err != nil {
			return mfaRequestFailure(err)
		}
		return writeWorkbenchOutput(rt, client, "json", out)
	case "totp":
		return startMFATOTP(ctx, client, rt, *show)
	default:
		var out sohaapi.MFAChallengeResultEnvelope
		if err := client.doJSON(ctx, http.MethodPost, "/api/v1/identity/mfa/challenges/"+*id+"/verify", client.Token, nil, sohaapi.MFAChallengeVerifyRequest{Response: response}, &out); err != nil {
			return mfaRequestFailure(err)
		}
		if !out.Data.Verified {
			return &authenticationError{Code: "mfa_unconfirmed", Message: "The server did not confirm MFA; do not assume the session was elevated"}
		}
		return writeWorkbenchOutput(rt, client, "json", out)
	}
}

func startMFATOTP(ctx context.Context, client APIClient, rt Runtime, show bool) error {
	var out sohaapi.MFAEnrollmentChallengeEnvelope
	if err := client.doJSON(ctx, http.MethodPost, "/api/v1/identity/mfa/totp/enroll", client.Token, nil, struct{}{}, &out); err != nil {
		return mfaRequestFailure(err)
	}
	id, err := uuid.Parse(out.Data.ChallengeID)
	if err != nil || id == uuid.Nil || id.String() != out.Data.ChallengeID || out.Data.ExpiresAt.IsZero() {
		return &authenticationError{Code: "mfa_unconfirmed", Message: "The server returned an invalid MFA challenge"}
	}
	data := map[string]any{"challengeId": out.Data.ChallengeID, "type": out.Data.Type, "expiresAt": out.Data.ExpiresAt}
	if show && out.Data.ProvisioningURI != "" {
		uri := out.Data.ProvisioningURI
		parsed, err := url.Parse(uri)
		if err != nil || len(uri) > 4096 || strings.ContainsFunc(uri, unicode.IsControl) || parsed.Scheme != "otpauth" || parsed.Host != "totp" || parsed.User != nil || parsed.Fragment != "" {
			return &authenticationError{Code: "mfa_unconfirmed", Message: "The server returned an invalid TOTP provisioning URI"}
		}
		data["provisioningUri"] = uri
	}
	return writeStructuredOutput(rt.Out, "json", map[string]any{"data": data})
}

func readMFAInput(rt Runtime) (string, error) {
	if file, ok := rt.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(rt.Err, "Verification code: "); err != nil {
			return "", err
		}
		value, err := readHiddenLine(rt, true)
		if err != nil {
			return "", fmt.Errorf("failed to read MFA response")
		}
		return readMFAResponse(strings.NewReader(value))
	}
	return readMFAResponse(rt.In)
}

func readMFAResponse(reader io.Reader) (string, error) {
	if reader == nil {
		return "", usageError{message: "verification response is required on stdin"}
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 131))
	if err != nil {
		return "", fmt.Errorf("failed to read MFA response")
	}
	value := strings.TrimSpace(string(raw))
	if len(raw) > 130 || value == "" || len(value) > 128 || strings.ContainsFunc(value, unicode.IsControl) {
		return "", usageError{message: "stdin must contain one bounded verification response"}
	}
	return value, nil
}

func mfaRequestFailure(err error) error {
	message := "MFA request failed; inspect the current session and original challenge before retrying"
	var status *httpStatusError
	if errors.As(err, &status) {
		message = fmt.Sprintf("MFA request returned HTTP %d; current-session verification is required", status.StatusCode)
	}
	return &authenticationError{Code: "mfa_request_failed", Message: message, cause: err}
}
