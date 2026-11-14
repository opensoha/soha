package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/crypto/bcrypt"
)

type networkPasswordFixture struct {
	user                 domainidentity.User
	hash                 string
	version              time.Time
	enabled, interactive bool
	err                  error
	lookup               string
}

func (f *networkPasswordFixture) GetByID(_ context.Context, id string) (domainidentity.User, error) {
	f.lookup = id
	return f.user, f.err
}
func (f *networkPasswordFixture) GetPasswordHash(context.Context, string) (string, error) {
	return f.hash, f.err
}
func (f *networkPasswordFixture) PasswordUpdatedAt(context.Context, string) (time.Time, error) {
	return f.version, f.err
}
func (f *networkPasswordFixture) LocalPasswordLoginEnabled(context.Context) (bool, error) {
	return f.enabled, f.err
}
func (f *networkPasswordFixture) NetworkPasswordRequiresInteraction(context.Context, string) (bool, error) {
	return f.interactive, f.err
}

func TestNetworkPasswordExactSubjectAndBytes(t *testing.T) {
	password := []byte(" a 密碼 ")
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := &networkPasswordFixture{user: domainidentity.User{ID: "user-1", Status: "active"}, hash: string(hash), version: time.Now().UTC(), enabled: true}
	v, err := NewNetworkPasswordVerifier(f, f)
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.Verify(context.Background(), "user-1", password)
	if err != nil || result.SubjectID != "user-1" || f.lookup != "user-1" {
		t.Fatalf("verification: %+v %v", result, err)
	}
	if _, err := v.Verify(context.Background(), "user-1", []byte("a 密碼")); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("trimmed password accepted: %v", err)
	}
	f.version = f.version.Add(time.Second)
	if err := v.Current(context.Background(), result); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("changed password receipt accepted: %v", err)
	}
}

func TestNetworkPasswordProtection(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"disabled", "inactive", "mfa", "identity-mismatch", "missing", "cancelled", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			f := &networkPasswordFixture{user: domainidentity.User{ID: "user-1", Status: "active"}, hash: string(hash), version: time.Now().UTC(), enabled: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "disabled":
				f.enabled = false
			case "inactive":
				f.user.Status = "disabled"
			case "mfa":
				f.interactive = true
			case "identity-mismatch":
				f.user.ID = "other"
			case "missing":
				f.err = apperrors.ErrNotFound
			case "cancelled":
				cancel()
			case "unavailable":
				f.err = errors.New("protection unavailable")
			}
			v, err := NewNetworkPasswordVerifier(f, f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Verify(ctx, "user-1", []byte("password")); err == nil {
				t.Fatal("protected authentication accepted")
			}
		})
	}
}
