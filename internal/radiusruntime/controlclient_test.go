package radiusruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := f(r)
	if err == nil {
		if response.Header == nil {
			response.Header = make(http.Header)
		}
		if response.Header.Get("Content-Type") == "" {
			response.Header.Set("Content-Type", "application/json")
		}
	}
	return response, err
}
func clientFixture(t *testing.T) (*ControlClient, *api.NetworkRADIUSRuntimeControl) {
	t.Helper()
	now := time.Now()
	configuration := api.NetworkRADIUSConfiguration{Reference: "approved-ref", TreeHash: "sha256:" + strings.Repeat("a", 64), HooksAttested: true, EapCARef: "ca-ref", EapServerCertificateRef: "cert-ref", EapServerKeyRef: "key-ref", NasSharedSecretRef: "secret-ref"}
	config := ClientConfiguration{URL: "https://control.invalid", RuntimeID: "runtime-1", MemberID: "member-1", PoolID: "pool-1", Configuration: configuration}
	control := &api.NetworkRADIUSRuntimeControl{PoolID: "pool-1", MemberID: "member-1", RuntimeID: "runtime-1", CredentialID: "credential-1", ProcessEpoch: "epoch-1", OwnerEpoch: 1, ControlRevision: 1, ConfigurationRevision: 1, ConfigurationHash: "sha256:" + strings.Repeat("b", 64), Configuration: configuration, NASBindingID: "binding-1", NASID: "nas-1", SiteID: "site-1", RunProcess: true, AcceptNewAuthentications: true, ServeExistingUntil: now.Add(time.Hour), ControlLeaseUntil: now.Add(10 * time.Second), ConfigurationLeaseUntil: now.Add(10 * time.Second)} // #nosec G101 -- Synthetic runtime credential identity for the local control-client fixture.
	client := &ControlClient{config: config, certificateUntil: now.Add(time.Hour), client: &http.Client{Timeout: time.Second}}
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		data, _ := json.Marshal(control)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})
	return client, control
}
func TestControlClientConsumesGeneratedControlAndDynamicProof(t *testing.T) {
	client, control := clientFixture(t)
	lease, err := client.Fetch(context.Background(), "epoch-1")
	if err != nil {
		t.Fatal(err)
	}
	if !lease.HardUntil.Equal(control.ServeExistingUntil) || lease.Control.Revision != 1 || lease.RuntimeProof == "" {
		t.Fatal(lease)
	}
	firstBinding := lease.Control.Binding
	control.ControlRevision++
	changed, err := client.Fetch(context.Background(), "epoch-1")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Control.Binding != firstBinding || !strings.Contains(changed.RuntimeProof, `"controlRevision":2`) || changed.RuntimeProof == lease.RuntimeProof {
		t.Fatal("proof not current or binding unstable")
	}
	control.CredentialID = "other-credential"
	changed, err = client.Fetch(context.Background(), "epoch-1")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Control.Binding == firstBinding {
		t.Fatal("credential change retained binding")
	}
}
func TestControlClientKnownZeroAndStoppedACKAreExplicit(t *testing.T) {
	client, _ := clientFixture(t)
	lease, err := client.Fetch(context.Background(), "epoch-1")
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"activeStates":0`) {
			t.Fatal("known zero omitted", string(data))
		}
		var ack api.NetworkRADIUSRuntimeACK
		if json.Unmarshal(data, &ack) != nil || !ack.ProcessStopped || !ack.StateCountKnown || ack.HooksReady || ack.Ready || ack.AcceptingNewAuthentications {
			t.Fatal("invalid stopped proof", string(data))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})
	zero := 0
	if err = client.Record(context.Background(), Observation{Epoch: "epoch-1", ProcessStopped: true, State: Snapshot{Revision: lease.Control.Revision, Known: true, Active: &zero}}); err != nil {
		t.Fatal(err)
	}
}

func TestControlClientStopOnlyWindowCannotGrantAuthentication(t *testing.T) {
	client, control := clientFixture(t)
	deadline := time.Now().Add(5 * time.Second)
	control.StopRequired = true
	control.StopConfirmUntil = &deadline
	control.ServeExistingUntil = time.Now().Add(-time.Second)
	control.ConfigurationLeaseUntil = time.Now().Add(-time.Second)
	lease, err := client.Fetch(context.Background(), "epoch-1")
	if err != nil || lease.RunProcess {
		t.Fatalf("invalid stop-only mapping %+v %v", lease, err)
	}
	control.StopConfirmUntil = nil
	if _, err = client.Fetch(context.Background(), "epoch-1"); err == nil {
		t.Fatal("expired authorization used outside stop-confirm window")
	}
}
func TestControlClientRejectsIdentityExpiryUnknownBodyAndStaleReadback(t *testing.T) {
	for _, kind := range []string{"epoch", "member", "config", "expired", "unknown-body", "oversize", "receipt"} {
		t.Run(kind, func(t *testing.T) {
			client, control := clientFixture(t)
			switch kind {
			case "epoch":
				control.ProcessEpoch = "foreign"
			case "member":
				control.MemberID = "foreign"
			case "config":
				control.Configuration.TreeHash = "sha256:" + strings.Repeat("c", 64)
			case "expired":
				control.ControlLeaseUntil = time.Now().Add(-time.Second)
			case "unknown-body":
				client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"unknown":true}`))}, nil
				})
			case "oversize":
				client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 65537)))}, nil
				})
			case "receipt":
				lease, err := client.Fetch(context.Background(), "epoch-1")
				if err != nil {
					t.Fatal(err)
				}
				client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					var ack api.NetworkRADIUSRuntimeACK
					_ = json.NewDecoder(r.Body).Decode(&ack)
					ack.ControlRevision++
					data, _ := json.Marshal(ack)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
				})
				zero := 0
				if client.Record(context.Background(), Observation{Epoch: "epoch-1", State: Snapshot{Revision: lease.Control.Revision, Known: true, Active: &zero}}) == nil {
					t.Fatal("stale receipt accepted")
				}
				return
			}
			if _, err := client.Fetch(context.Background(), "epoch-1"); err == nil {
				t.Fatal("untrusted control accepted")
			}
		})
	}
}
