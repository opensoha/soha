package networkproxy

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	access "github.com/opensoha/soha/internal/domain/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	operation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

func TestCommandWindowAndTargets(t *testing.T) {
	now := time.Now().UTC()
	valid := domain.RuntimeCommandInput{RequestID: "request-1", ProcessEpoch: "epoch-1", Kind: "groups", IssuedAt: now, ExpiresAt: now.Add(30 * time.Second)}
	cases := []struct {
		name   string
		modify func(*domain.RuntimeCommandInput)
		want   bool
	}{
		{"valid", func(*domain.RuntimeCommandInput) {}, true},
		{"future issued", func(c *domain.RuntimeCommandInput) { c.IssuedAt = now.Add(time.Second) }, false},
		{"caller extends deadline", func(c *domain.RuntimeCommandInput) { c.ExpiresAt = now.Add(31 * time.Second) }, false},
		{"expired", func(c *domain.RuntimeCommandInput) { c.ExpiresAt = now }, false},
		{"missing epoch", func(c *domain.RuntimeCommandInput) { c.ProcessEpoch = "" }, false},
		{"arbitrary operation", func(c *domain.RuntimeCommandInput) { c.Kind = "/configs" }, false},
		{"unbounded logs", func(c *domain.RuntimeCommandInput) { c.Kind = "logs"; c.Limit = 201 }, false},
		{"probe requires profile", func(c *domain.RuntimeCommandInput) { c.Kind = "probe" }, false},
		{"select requires target", func(c *domain.RuntimeCommandInput) { c.Kind = "select" }, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := valid
			tt.modify(&c)
			if got := validateCommand(c, now) == nil; got != tt.want {
				t.Fatalf("valid=%v want %v", got, tt.want)
			}
		})
	}
}
func TestRateRequiresContinuousEpoch(t *testing.T) {
	now := time.Now().UTC()
	base := domain.TrafficSample{ProcessEpoch: "e1", SampleSequence: 1, ObservedAt: now, UptimeSeconds: 10, UploadTotal: 100, DownloadTotal: 100}
	cases := []struct {
		name   string
		modify func(*domain.TrafficSample)
		state  string
	}{
		{"continuous", func(*domain.TrafficSample) {}, "known"},
		{"new process", func(s *domain.TrafficSample) { s.ProcessEpoch = "e2" }, "unknown"},
		{"legacy epoch", func(s *domain.TrafficSample) { s.ProcessEpoch = "" }, "unknown"},
		{"reordered", func(s *domain.TrafficSample) { s.SampleSequence = 1 }, "unknown"},
		{"skipped sequence", func(s *domain.TrafficSample) { s.SampleSequence = 3 }, "gap"},
		{"counter reset", func(s *domain.TrafficSample) { s.UploadTotal = 10 }, "unknown"},
		{"gap", func(s *domain.TrafficSample) { s.ObservedAt = now.Add(61 * time.Second) }, "gap"},
		{"same timestamp", func(s *domain.TrafficSample) { s.ObservedAt = now }, "unknown"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			next := base
			next.SampleSequence = 2
			next.ObservedAt = now.Add(10 * time.Second)
			next.UptimeSeconds = 20
			next.UploadTotal = 200
			next.DownloadTotal = 300
			tt.modify(&next)
			samples := []domain.TrafficSample{base, next}
			calculateTrafficRates(samples)
			if samples[1].RateState != tt.state {
				t.Fatalf("rate state %s", samples[1].RateState)
			}
			if samples[0].RateState != "unknown" || samples[0].UploadBytesPerSecond != 0 || samples[0].DownloadBytesPerSecond != 0 {
				t.Fatal("first rate invented")
			}
			if tt.state == "known" && (samples[1].UploadBytesPerSecond != 10 || samples[1].DownloadBytesPerSecond != 20) {
				t.Fatal("incorrect rate")
			}
		})
	}
}
func TestExternalDescriptorDoesNotImpersonateBuiltin(t *testing.T) {
	if validateDescriptor(domain.EngineExternal, nil) == nil {
		t.Fatal("external descriptor optional")
	}
	d := &domain.EngineDescriptor{EngineID: "custom", AdapterID: "operator", Version: "1", ProtocolMajor: 1, Capabilities: []string{"traffic"}}
	if err := validateDescriptor(domain.EngineExternal, d); err != nil {
		t.Fatal(err)
	}
	d.Capabilities = []string{"close_connection"}
	if validateDescriptor(domain.EngineExternal, d) == nil {
		t.Fatal("close without connections accepted")
	}
}

type commandScope struct{ allowed bool }

func (s commandScope) Authorize(context.Context, access.Request) (access.Decision, error) {
	return access.Decision{Allowed: s.allowed}, nil
}

type commandOperations struct{}

func (commandOperations) Record(context.Context, operation.Entry) error { return nil }

type commandManagementStore struct {
	ManagementStore
	now    time.Time
	queued bool
}

func (s *commandManagementStore) Get(context.Context, string) (domain.Instance, error) {
	return domain.Instance{ID: "p1", ProcessEpoch: "e1", LastSampleAt: &s.now, Capabilities: []string{"select", "groups", "probe"}}, nil
}
func (s *commandManagementStore) QueueCommand(context.Context, string, string, domain.RuntimeCommandInput, time.Time) (domain.RuntimeCommandReceipt, error) {
	s.queued = true
	return domain.RuntimeCommandReceipt{Status: "pending"}, nil
}
func (s *commandManagementStore) GetCommand(context.Context, string, string, time.Time) (domain.RuntimeCommandReceipt, error) {
	return domain.RuntimeCommandReceipt{}, nil
}
func (s *commandManagementStore) ClaimCommand(context.Context, string, string, time.Time) (*domain.RuntimeCommand, error) {
	return nil, nil
}
func (s *commandManagementStore) CompleteCommand(context.Context, string, domain.RuntimeCommandResult, time.Time) error {
	return nil
}
func TestCommandPermissionsAndScopeRemainIndependent(t *testing.T) {
	now := time.Now().UTC()
	p := identity.Principal{UserID: "u1", Roles: []string{"viewer"}}
	for _, tt := range []struct {
		name, permission, kind string
		scope, allowed         bool
	}{{"update only select", appaccess.PermNetworkAccessProxyInstancesUpdate, "select", true, true}, {"view cannot select", appaccess.PermNetworkAccessProxyInstancesView, "select", true, false}, {"view groups", appaccess.PermNetworkAccessProxyInstancesView, "groups", true, true}, {"scope denied", appaccess.PermNetworkAccessProxyInstancesUpdate, "select", false, false}, {"audit unavailable", appaccess.PermNetworkAccessProxyInstancesUpdate, "select", true, false}, {"unapproved probe", appaccess.PermNetworkAccessProxyInstancesView, "probe", true, false}} {
		t.Run(tt.name, func(t *testing.T) {
			store := &commandManagementStore{now: now}
			s := &Management{store: store, permissions: appaccess.NewPermissionResolver(proxyTestRoles{keys: []string{tt.permission}}), scope: commandScope{tt.scope}, audit: closeTestAudit{}, operations: commandOperations{}, now: func() time.Time { return now }}
			if tt.name == "audit unavailable" {
				s.audit = closeTestAudit{err: errors.New("audit unavailable")}
			}
			input := domain.RuntimeCommandInput{RequestID: "r1", ProcessEpoch: "e1", Kind: domain.RuntimeCommandInput{}.Kind, IssuedAt: now, ExpiresAt: now.Add(20 * time.Second), GroupID: "g1", NodeID: "n1", ProbeProfileID: "approved-only"}
			switch tt.kind {
			case "select":
				input.Kind = "select"
			case "groups":
				input.Kind = "groups"
			case "probe":
				input.Kind = "probe"
			}
			_, err := s.QueueCommand(context.Background(), p, "p1", input)
			if (err == nil) != tt.allowed || store.queued != tt.allowed {
				t.Fatalf("error=%v queued=%v", err, store.queued)
			}
			if tt.name == "view cannot select" && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatal(err)
			}
		})
	}
}
