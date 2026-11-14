package sohacli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// authenticationError keeps transport details available to callers without
// displaying response bodies that may echo a credential.
type authenticationError struct {
	Code    string
	Message string
	cause   error
}

func (e *authenticationError) Error() string { return e.Code + ": " + e.Message }
func (e *authenticationError) Unwrap() error { return e.cause }

func refreshFailure(err error) error {
	var classified *authenticationError
	if errors.As(err, &classified) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	code, message := "auth_refresh_unavailable", "session refresh is temporarily unavailable; retry when the server is reachable; stored credentials were retained"
	var status *httpStatusError
	if errors.As(err, &status) {
		switch status.StatusCode {
		case http.StatusUnauthorized:
			code, message = "auth_refresh_rejected", "the server rejected the refresh credential; check session expiry, revocation and server signing-key/database continuity, then log in again"
		case http.StatusForbidden:
			code, message = "auth_refresh_forbidden", "the server denied session refresh; check account and authentication policy"
		default:
			message = fmt.Sprintf("session refresh returned HTTP %d; stored credentials were retained", status.StatusCode)
		}
	}
	return &authenticationError{Code: code, Message: message, cause: err}
}

// A server error may echo a submitted credential without labeling it. Remove
// known credentials by value before exposing any server-provided error text.
func safeAPIErrorMessage(status string, raw, request []byte, known ...string) string {
	if len(request) > 1<<20 {
		return status
	}
	var body any
	if json.Unmarshal(request, &body) == nil {
		var visit func(any, int)
		visit = func(value any, depth int) {
			if depth > 16 {
				return
			}
			switch v := value.(type) {
			case map[string]any:
				for key, child := range v {
					normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
					switch normalized {
					case "password", "accesstoken", "refreshtoken", "token", "apikey", "authorization", "devicecode", "usercode", "verificationhandle", "codeverifier", "confirmationid":
						if secret, ok := child.(string); ok {
							known = append(known, secret)
						}
					}
					visit(child, depth+1)
				}
			case []any:
				for _, child := range v {
					visit(child, depth+1)
				}
			}
		}
		visit(body, 0)
	}
	message := responseErrorMessage(status, raw)
	for _, secret := range known {
		if secret == "" {
			continue
		}
		message = strings.ReplaceAll(message, secret, "[REDACTED]")
		escaped, _ := json.Marshal(secret)
		if len(escaped) > 2 {
			message = strings.ReplaceAll(message, string(escaped[1:len(escaped)-1]), "[REDACTED]")
		}
	}
	message = terminalLabel(redactSensitiveText(message))
	if len(message) > 2048 {
		message = message[:2048] + "..."
	}
	return message
}
