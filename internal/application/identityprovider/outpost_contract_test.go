package identityprovider

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
)

func TestOutpostConfigPayloadMatchesAgentVerifierOrder(t *testing.T) {
	issuedAt := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	config := sohaapi.IdentityOutpostRuntimeConfig{
		OutpostID: "outpost-1", ProtocolVersion: "v1", ConfigurationVersion: 42,
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(10 * time.Minute), KeyID: "outpost-key-1",
		CheckURL: "/identity/outposts/outpost-1/check",
		Routes:   []sohaapi.IdentityOutpostRoute{{ApplicationID: "app-1", Host: "app.example.com", PathPrefix: "/", ProviderID: "provider-1", SkipPaths: []string{"/public"}}},
	}
	payload, err := outpostConfigPayload(config)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"outpostId":"outpost-1","protocolVersion":"v1","configurationVersion":42,"issuedAt":"2026-07-27T10:00:00Z","expiresAt":"2026-07-27T10:10:00Z","keyId":"outpost-key-1","checkUrl":"/identity/outposts/outpost-1/check","routes":[{"applicationId":"app-1","host":"app.example.com","pathPrefix":"/","providerId":"provider-1","skipPaths":["/public"]}]}`
	if string(payload) != want {
		t.Fatalf("payload = %s\nwant    = %s", payload, want)
	}
	seed := sha256.Sum256([]byte("test-outpost-signing-secret"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	signature := ed25519.Sign(privateKey, payload)
	config.Signature = base64.StdEncoding.EncodeToString(signature)
	if !ed25519.Verify(privateKey.Public().(ed25519.PublicKey), payload, signature) {
		t.Fatal("agent-compatible signature did not verify")
	}
}

func TestOutpostRuntimeVersionIsStableAndMonotonic(t *testing.T) {
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	providers := []domainprovider.Provider{{ID: "provider-1", ApplicationID: "app-1", Config: map[string]any{"externalHosts": []string{"app.example.com"}}}}

	first, err := service.resolveOutpostConfigurationVersion(context.Background(), "outpost-1", providers)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := service.resolveOutpostConfigurationVersion(context.Background(), "outpost-1", providers)
	if err != nil {
		t.Fatal(err)
	}
	providers[0].Config["externalHosts"] = []string{"changed.example.com"}
	changed, err := service.resolveOutpostConfigurationVersion(context.Background(), "outpost-1", providers)
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.resolveOutpostConfigurationVersion(context.Background(), "outpost-1", providers)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || stable != first || changed != first+1 || again != changed {
		t.Fatalf("versions = %d, %d, %d, %d; want 1, 1, 2, 2", first, stable, changed, again)
	}
}
