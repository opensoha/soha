package plugins_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"reflect"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

// This detached signature is a cross-language test vector for author tools.
func TestExecutableReleaseVector(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	body := read("../fixtures/json-schema/plugin-release-attestation/valid/package.json")
	signature := read("testdata/release-v1/soha-plugin-attestation.sig")
	block, _ := pem.Decode(read("testdata/release-v1/publisher.pem"))
	if block == nil {
		t.Fatal("invalid test public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.(ed25519.PublicKey)
	if !ed25519.Verify(pub, body, signature) || ed25519.Verify(pub, append(body, ' '), signature) {
		t.Fatal("signature must match only the exact attestation bytes")
	}
	var att sohaapi.PluginReleaseAttestation
	if err := json.Unmarshal(body, &att); err != nil {
		t.Fatal(err)
	}
	manifestBytes := read("../fixtures/json-schema/plugin-manifest/valid/executable-package.json")
	var manifest sohaapi.PluginManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(manifestBytes)
	if manifest.Components == nil || att.PluginID != manifest.ID || att.Version != manifest.Version ||
		att.RawManifestSha256 != hex.EncodeToString(digest[:]) || !reflect.DeepEqual(att.Components, *manifest.Components) {
		t.Fatal("signed inventory must exactly match the pinned manifest")
	}
}
