package networkproxyruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

// The message preserves the existing caller error; diagnostic fields never
// contain an upstream error, body, identity, destination or credential.
type ingressCheckError struct {
	message, classification string
	status                  int
	decoded, allowed        bool
	reason                  string
}

func (e *ingressCheckError) Error() string { return e.message }

func ingressDecisionError(classification, message string, result api.NetworkProxyIngressCheckResult) error {
	return &ingressCheckError{message: message, classification: classification, decoded: true, allowed: result.Allowed, reason: result.ReasonCode}
}

type operationStatusError int

func (e operationStatusError) Error() string {
	return fmt.Sprintf("proxy operation returned HTTP %d", e)
}

func ingressFailure(err error) (string, int, *ingressCheckError) {
	var detail *ingressCheckError
	if errors.As(err, &detail) {
		switch detail.classification {
		case "response_invalid", "denied", "identity_mismatch", "decision_expired", "local_lease_expired":
			return detail.classification, detail.status, detail
		}
	}
	var control controlStatusError
	if errors.As(err, &control) {
		return "http_status", int(control), nil
	}
	var operation operationStatusError
	if errors.As(err, &operation) {
		return "http_status", int(operation), nil
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", 0, nil
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout", 0, nil
	}
	return "transport", 0, nil
}

func logIngressClosure(stage string, network api.NetworkProxyIngressCheckInputNetwork, elapsed time.Duration, err error) {
	classification, status, detail := ingressFailure(err)
	switch stage {
	case "ingress.check", "ingress.lease":
	case "ingress.refresh":
		classification, status, detail = "refresh_failed", 0, nil
	default:
		stage = "ingress.lease"
	}
	if network != "tcp" && network != "udp" {
		network = "unknown"
	}
	fields := []any{"stage", stage, "network", network, "classification", classification, "elapsedMs", elapsed.Milliseconds()}
	if status != 0 {
		fields = append(fields, "status", status)
	}
	if detail != nil && detail.decoded {
		reason := "masked"
		if detail.reason == "access_denied" {
			reason = "access_denied"
		}
		fields = append(fields, "allowed", detail.allowed, "reasonCode", reason)
	}
	var traced *ingressHTTPError
	if errors.As(err, &traced) {
		fields = append(fields, traced.trace.fields()...)
	}
	slog.Warn("proxy ingress closed", fields...)
}
