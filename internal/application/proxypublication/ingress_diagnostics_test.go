package proxypublication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
)

type diagnosticCredentialStore struct{ *bundleFake }

func (s diagnosticCredentialStore) GetCredential(context.Context, string) (domain.Credential, error) {
	return domain.Credential{}, errors.New("NEVER_LOG_secret_target")
}

type diagnosticDesired struct {
	desiredFake
	calls int
}

func (d *diagnosticDesired) ObserveMemberConfigurations(ctx context.Context, ids []string) ([]proxy.MemberConfigurationState, error) {
	d.calls++
	if d.calls > 1 {
		return nil, errors.New("NEVER_LOG_secret_target")
	}
	return d.desiredFake.ObserveMemberConfigurations(ctx, ids)
}

func TestIngressDenialDiagnosticsAreStaticAndDoNotChangeDecisions(t *testing.T) {
	const secret = "NEVER_LOG_secret_target"
	for _, stage := range []string{"runtime_authorization", "request_invalid", "credential_lookup", "credential_secret", "current_bundle", "runtime_member", "credential_scope", "runtime_readback", "endpoint_context", "destination_authorization", "serving_lease", "success"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			defer slog.SetDefault(previous)
			b, f, input := ingressFixture()
			input.DestinationHost = secret
			mutateIngressDiagnostic(t, stage, secret, b, f, &input)
			out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
			if stage == "success" {
				if err != nil || !out.Allowed || output.Len() != 0 {
					t.Fatalf("success changed: allowed=%v err=%v log=%s", out.Allowed, err, output.String())
				}
				return
			}
			if out.Allowed {
				t.Fatal("denial became allowed")
			}
			if stage == "runtime_authorization" {
				if err == nil || err.Error() != secret {
					t.Fatal("runtime error changed")
				}
			} else if err != nil {
				t.Fatal("wire error changed")
			}
			assertStaticIngressLog(t, &output, stage, secret, input.CredentialSecret)
			if out.ReasonCode != "access_denied" {
				t.Fatalf("wire reason changed: %v", out.ReasonCode)
			}
		})
	}
}

func mutateIngressDiagnostic(t *testing.T, stage, secret string, b *Bundles, f *bundleFake, input *api.NetworkProxyIngressCheckInput) {
	t.Helper()
	switch stage {
	case "runtime_authorization":
		b.runtime = runtimeFake{err: errors.New(secret)}
	case "request_invalid":
		input.Network = api.NetworkProxyIngressCheckInputNetwork(secret)
	case "credential_lookup":
		b.store = diagnosticCredentialStore{f}
	case "credential_secret":
		input.CredentialSecret = secret
	case "current_bundle":
		f.record.Public.State = api.NetworkProxyBundleState(secret)
	case "runtime_member":
		states := fixtureValue[desiredFake](t, b.desired)
		states.states[0].AppliedHash = secret
		b.desired = states
	case "credential_scope":
		f.credential.ExpiresAt = b.now()
	case "runtime_readback":
		input.ProcessEpoch = secret
	case "endpoint_context":
		b.current = currentFake{err: errors.New(secret)}
	case "destination_authorization":
		b.destination = destinationFake{err: errors.New(secret)}
	case "serving_lease":
		b.desired = &diagnosticDesired{desiredFake: fixtureValue[desiredFake](t, b.desired)}
	}
}

func assertStaticIngressLog(t *testing.T, output *bytes.Buffer, stage, secret, credentialSecret string) {
	t.Helper()
	if strings.Contains(output.String(), secret) || strings.Contains(output.String(), credentialSecret) || strings.Contains(output.String(), "proxy-1") {
		t.Fatal("log leaked error/credential/identity")
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("expected one static stage log: %s", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["stage"] != stage {
		t.Fatalf("stage=%v want=%s", record["stage"], stage)
	}
	for key := range record {
		switch key {
		case "time", "level", "msg", "stage":
		default:
			t.Fatalf("unexpected field %s", key)
		}
	}
}
