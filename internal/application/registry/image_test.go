package registry

import (
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
)

func TestVerifyBuildImageAuthenticatesAndHashesManifest(t *testing.T) {
	manifest := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":%q,"digest":%q,"size":2},"layers":[]}`, ocispec.MediaTypeImageManifest, ocispec.MediaTypeImageConfig, digest.FromString("{}")))
	wantDigest := digest.FromBytes(manifest).String()
	var corrupt, redirect atomic.Bool
	var authenticated atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/team/app/manifests/"+wantDigest {
			t.Errorf("unexpected registry path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		username, password, _ := r.BasicAuth()
		if username != "builder" || password != "test-registry-secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		if redirect.Load() {
			http.Redirect(w, r, "/unexpected", http.StatusTemporaryRedirect)
			return
		}
		data := append([]byte(nil), manifest...)
		if corrupt.Load() {
			data[len(data)-2] = 'x'
		}
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Header().Set("Docker-Content-Digest", wantDigest)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	connection := domainregistry.Connection{ID: "private", Endpoint: server.URL, Namespace: "team", Username: "builder", Secret: "test-registry-secret", Metadata: map[string]any{"allowedCIDRs": "127.0.0.1/32", "caCertificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}}
	repo := &captureRegistryRepository{}
	service := New(repo, nil, WithCredentialEncryptionKey("stable-test-key-32-bytes-or-more"))
	if err := service.encryptSecret(&connection); err != nil {
		t.Fatal(err)
	}
	repo.items = []domainregistry.Connection{connection}
	image := strings.TrimPrefix(server.URL, "https://") + "/team/app:v1"
	if err := service.VerifyBuildImage(t.Context(), "private", image, wantDigest); err != nil || authenticated.Load() != 1 {
		t.Fatalf("actual authenticated manifest rejected: %v requests=%d", err, authenticated.Load())
	}
	corrupt.Store(true)
	if err := service.VerifyBuildImage(t.Context(), "private", image, wantDigest); err == nil {
		t.Fatal("trusted a digest header with different body bytes")
	}
	corrupt.Store(false)
	redirect.Store(true)
	if err := service.VerifyBuildImage(t.Context(), "private", image, wantDigest); err == nil {
		t.Fatal("followed a registry redirect")
	}
	before := authenticated.Load()
	for _, unauthorizedImage := range []string{"other.example/team/app:v1", strings.Replace(image, "/team/", "/outside/", 1)} {
		if err := service.VerifyBuildImage(t.Context(), "private", unauthorizedImage, wantDigest); err == nil {
			t.Fatal("accepted image outside registered endpoint/namespace")
		}
	}
	repo.items[0].Metadata = map[string]any{}
	if err := service.VerifyBuildImage(t.Context(), "private", image, wantDigest); err == nil || authenticated.Load() != before {
		t.Fatal("accessed private registry without its explicit network grant")
	}
	if scrubConnection(connection).Metadata["caCertificate"] != connection.Metadata["caCertificate"] {
		t.Fatal("registry network configuration lost during round trip")
	}
	for _, bad := range []struct{ mediaType, data string }{
		{"text/plain", string(manifest)},
		{ocispec.MediaTypeImageManifest, `{}`},
		{ocispec.MediaTypeImageManifest, strings.Replace(string(manifest), ocispec.MediaTypeImageConfig, "application/example", 1)},
		{ocispec.MediaTypeImageIndex, `{"schemaVersion":2,"manifests":[]}`},
		{ocispec.MediaTypeImageIndex, fmt.Sprintf(`{"schemaVersion":2,"manifests":[{"mediaType":"text/plain","size":2,"digest":%q}]}`, digest.FromString("{}"))},
	} {
		if validateImageManifest(bad.mediaType, []byte(bad.data)) == nil {
			t.Fatal("accepted non-image artifact", bad.mediaType)
		}
	}
}
