package proxypublication

import (
	"context"
	"errors"
	identity "github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type bindingAuth struct {
	err      error
	instance string
	calls    int
}

func (a *bindingAuth) Authorize(_ context.Context, _ identity.Principal, instance, _ string) error {
	a.calls++
	a.instance = instance
	return a.err
}

func TestMihomoBundleBindingValidatesCurrentReferenceAndRecipient(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Bundles, *bundleFake, *access.MihomoProfileInput, *access.Device)
		want   error
	}{
		{name: "active authorized recipient"},
		{name: "inactive", mutate: func(_ *Bundles, f *bundleFake, _ *access.MihomoProfileInput, _ *access.Device) {
			f.record.Public.State = "client_eligible"
		}, want: apperrors.ErrAccessDenied},
		{name: "mode", mutate: func(_ *Bundles, _ *bundleFake, in *access.MihomoProfileInput, _ *access.Device) {
			in.DeliveryMode = "mapped_nodes"
		}, want: apperrors.ErrConflict},
		{name: "source revision", mutate: func(_ *Bundles, _ *bundleFake, in *access.MihomoProfileInput, _ *access.Device) { in.SourceRevision++ }, want: apperrors.ErrConflict},
		{name: "org revision", mutate: func(_ *Bundles, _ *bundleFake, in *access.MihomoProfileInput, _ *access.Device) { in.OrgRevision++ }, want: apperrors.ErrConflict},
		{name: "device outside scope", mutate: func(_ *Bundles, _ *bundleFake, in *access.MihomoProfileInput, d *access.Device) {
			in.DeviceID = "device-2"
			d.ID = "device-2"
		}, want: apperrors.ErrAccessDenied},
		{name: "device owner outside scope", mutate: func(_ *Bundles, _ *bundleFake, _ *access.MihomoProfileInput, d *access.Device) {
			d.OwnerUserID = "user-2"
		}, want: apperrors.ErrAccessDenied},
		{name: "untrusted device mismatch", mutate: func(_ *Bundles, _ *bundleFake, _ *access.MihomoProfileInput, d *access.Device) { d.ID = "device-2" }, want: apperrors.ErrAccessDenied},
		{name: "instance access denied", mutate: func(b *Bundles, _ *bundleFake, _ *access.MihomoProfileInput, _ *access.Device) {
			b.auth.(*bindingAuth).err = apperrors.ErrAccessDenied
		}, want: apperrors.ErrAccessDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, f, _ := ingressFixture()
			b.auth = &bindingAuth{}
			f.record.Public.DeliveryMode = "direct_subscription"
			input := access.MihomoProfileInput{BundleID: "bundle-1", DeliveryMode: "direct_subscription", OrgRevision: 1, SourceRevision: 1, DeviceID: "device-1"}
			device := access.Device{ID: "device-1", OwnerUserID: "user-1"}
			if tt.mutate != nil {
				tt.mutate(b, f, &input, &device)
			}
			err := b.ValidateMihomoBundle(context.Background(), identity.Principal{UserID: "administrator"}, input, device, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("validation error = %v, want %v", err, tt.want)
			}
			if b.auth.(*bindingAuth).instance != "master" {
				t.Fatal("permission did not use owning instance")
			}
		})
	}
}

func retainedBindingFixture() (*Bundles, *bundleFake, access.MihomoProfileInput, access.Device, *access.MihomoProfile) {
	b, f, _ := ingressFixture()
	b.auth = &bindingAuth{err: apperrors.ErrAccessDenied}
	f.record.Public.DeliveryMode = "direct_subscription"
	input := access.MihomoProfileInput{BundleID: "bundle-1", DeviceID: "device-1", DeliveryMode: "direct_subscription", SourceRevision: 1, OrgRevision: 1, Mode: access.MihomoModeManagedFollow}
	device := access.Device{ID: "device-1", OwnerUserID: "user-1"}
	previous := &access.MihomoProfile{ID: "profile-1", Revision: 4, BundleID: input.BundleID, DeviceID: input.DeviceID, DeliveryMode: input.DeliveryMode, SourceRevision: 1, OrgRevision: 1, Mode: input.Mode}
	return b, f, input, device, previous
}
func TestMihomoRetainedBindingWithoutProxyView(t *testing.T) {
	b, _, input, device, previous := retainedBindingFixture()
	input.Name = "Renamed"
	if err := b.ValidateMihomoBundle(context.Background(), identity.Principal{UserID: "editor"}, input, device, previous); err != nil || b.auth.(*bindingAuth).calls != 0 {
		t.Fatal("unchanged binding still needs view", err)
	}
}
func TestMihomoNewOrChangedBindingStillRequiresProxyView(t *testing.T) {
	cases := map[string]func(*access.MihomoProfileInput, **access.MihomoProfile){"create": func(_ *access.MihomoProfileInput, p **access.MihomoProfile) { *p = nil }, "bundle": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.BundleID = "other" }, "device": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.DeviceID = "other" }, "delivery": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.DeliveryMode = "mapped_nodes" }, "org": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.OrgRevision++ }, "source": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.SourceRevision++ }, "sourceType": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.SourceType = "manual_node" }, "mode": func(in *access.MihomoProfileInput, _ **access.MihomoProfile) { in.Mode = "app_subscription" }, "emptyOldBundle": func(_ *access.MihomoProfileInput, p **access.MihomoProfile) { (*p).BundleID = "" }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b, _, input, device, previous := retainedBindingFixture()
			change(&input, &previous)
			err := b.ValidateMihomoBundle(context.Background(), identity.Principal{}, input, device, previous)
			if !errors.Is(err, apperrors.ErrAccessDenied) || b.auth.(*bindingAuth).calls != 1 {
				t.Fatal("changed binding bypassed view", err)
			}
		})
	}
}
func TestRetainedMihomoBindingRechecksCurrentAuthorization(t *testing.T) {
	for _, change := range []func(*Bundles, *bundleFake, *access.Device){func(_ *Bundles, f *bundleFake, _ *access.Device) { f.record.Public.State = "revoked" }, func(_ *Bundles, _ *bundleFake, d *access.Device) { d.OwnerUserID = "other" }, func(_ *Bundles, _ *bundleFake, d *access.Device) { d.ID = "other" }, func(b *Bundles, _ *bundleFake, _ *access.Device) { b.sources.(*sourceFake).allowed = []string{} }} {
		b, f, input, device, previous := retainedBindingFixture()
		change(b, f, &device)
		if err := b.ValidateMihomoBundle(context.Background(), identity.Principal{}, input, device, previous); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatal("retention bypassed current authorization", err)
		}
	}
}
