package storagedriverhost

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	contract "github.com/opensoha/soha/contracts/plugins"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	manifestName    = "plugin.manifest.json"
	attestationName = "soha-plugin-attestation.json"
	signatureName   = "soha-plugin-attestation.sig"
	publisherName   = "soha-plugin-publisher.pem"
	maxDocument     = 1 << 20
	maxArtifact     = 256 << 20
)

type releaseArtifact struct {
	Name       string `json:"name"`
	ByteLength int64  `json:"byteLength"`
	SHA256     string `json:"sha256"`
}

type releaseBackend struct {
	DriverID  string                     `json:"driverId"`
	Protocol  string                     `json:"protocol"`
	Platforms map[string]releaseArtifact `json:"platforms"`
}

type releaseComponents struct {
	Backend *releaseBackend  `json:"backend"`
	Web     *releaseArtifact `json:"web"`
}

type releaseManifest struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Version      string                `json:"version"`
	Runtime      struct{ Mode string } `json:"runtime"`
	Components   releaseComponents     `json:"components"`
	ConfigSchema json.RawMessage       `json:"configSchema"`
}

type releaseAttestation struct {
	PluginID          string            `json:"pluginId"`
	Version           string            `json:"version"`
	Repository        string            `json:"repository"`
	Commit            string            `json:"commitSha"`
	RawManifestSHA256 string            `json:"rawManifestSha256"`
	Components        releaseComponents `json:"components"`
}

type verifiedRelease struct {
	descriptor        Descriptor
	identity          storagedriver.Identity
	binary            releaseArtifact
	attestationDigest [sha256.Size]byte
	publisherDigest   [sha256.Size]byte
}

type releaseSchemas struct {
	manifest    *jsonschema.Schema
	attestation *jsonschema.Schema
}

var loadReleaseSchemas = sync.OnceValues(func() (releaseSchemas, error) {
	const prefix = "https://contracts.opensoha.dev/plugins/"
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	manifest := prefix + "plugin-manifest.schema.json"
	attestation := prefix + "plugin-release-attestation.schema.json"
	for url, raw := range map[string][]byte{manifest: contract.ManifestSchema(), attestation: contract.ReleaseAttestationSchema()} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return releaseSchemas{}, err
		}
		if url == manifest {
			properties, err := executableManifestProperties(document)
			if err != nil {
				return releaseSchemas{}, err
			}
			// A companion pack is never a storage driver. Exclude its
			// referenced schema so storage release verification stays offline.
			properties["companionPack"] = false
		}
		if err := compiler.AddResource(url, document); err != nil {
			return releaseSchemas{}, err
		}
	}
	schema, err := compiler.Compile(manifest)
	if err != nil {
		return releaseSchemas{}, err
	}
	attest, err := compiler.Compile(attestation)
	return releaseSchemas{manifest: schema, attestation: attest}, err
})

func executableManifestProperties(document any) (map[string]any, error) {
	schema, _ := document.(map[string]any)
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("driver manifest schema properties missing")
	}
	return properties, nil
}

func verifyRelease(selection DriverConfig, keyPEM, rawAttestation, signature, rawManifest []byte, platform string) (verifiedRelease, error) {
	key, err := parsePublicKey(keyPEM)
	if err != nil {
		return verifiedRelease{}, err
	}
	if !ed25519.Verify(key, rawAttestation, signature) {
		return verifiedRelease{}, errors.New("invalid storage driver release signature")
	}
	schemas, err := loadReleaseSchemas()
	if err != nil {
		return verifiedRelease{}, err
	}
	var attestation releaseAttestation
	if !decodeReleaseDocument(rawAttestation, schemas.attestation, &attestation) {
		return verifiedRelease{}, errors.New("invalid storage driver release attestation")
	}
	digest := sha256.Sum256(rawManifest)
	if attestation.Repository != selection.Repository || attestation.Commit != selection.Commit ||
		attestation.PluginID != selection.PluginID || attestation.Version != selection.Version ||
		attestation.RawManifestSHA256 != hex.EncodeToString(digest[:]) {
		return verifiedRelease{}, errors.New("storage driver attestation does not match the pinned release")
	}
	release, err := verifyManifest(rawManifest, schemas.manifest, attestation, platform)
	release.attestationDigest = sha256.Sum256(rawAttestation)
	release.publisherDigest = sha256.Sum256(key)
	return release, err
}

func parsePublicKey(raw []byte) (ed25519.PublicKey, error) {
	trimmed := bytes.TrimSpace(raw)
	block, rest := pem.Decode(trimmed)
	invalid := errors.New("invalid configured Ed25519 publisher key")
	const begin = "-----BEGIN PUBLIC KEY-----"
	if !bytes.HasPrefix(trimmed, []byte(begin)) || bytes.Count(trimmed, []byte("-----BEGIN ")) != 1 ||
		block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, invalid
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, invalid
	}
	edKey, ok := key.(ed25519.PublicKey)
	if !ok || len(edKey) != ed25519.PublicKeySize {
		return nil, invalid
	}
	return edKey, nil
}

func verifyManifest(raw []byte, schema *jsonschema.Schema, attest releaseAttestation, platform string) (verifiedRelease, error) {
	var manifest releaseManifest
	if !decodeReleaseDocument(raw, schema, &manifest) {
		return verifiedRelease{}, errors.New("invalid signed storage driver manifest")
	}
	backend := manifest.Components.Backend
	if manifest.ID != attest.PluginID || manifest.Version != attest.Version ||
		manifest.Runtime.Mode != "executable-package-v1" || backend == nil ||
		!reflect.DeepEqual(manifest.Components, attest.Components) {
		return verifiedRelease{}, errors.New("signed storage driver identity or inventory mismatch")
	}
	if err := validateInventory(manifest.Components); err != nil {
		return verifiedRelease{}, err
	}
	binary, ok := backend.Platforms[platform]
	if !ok {
		return verifiedRelease{}, errors.New("signed storage driver has no artifact for this platform")
	}
	return verifiedRelease{
		descriptor: Descriptor{ID: backend.DriverID, Name: manifest.Name, ConfigSchema: manifest.ConfigSchema},
		identity: storagedriver.Identity{
			PluginID: attest.PluginID, Version: attest.Version, ProtocolMajor: storagedriver.ProtocolMajor,
			Capabilities: storagedriver.V1Capabilities,
		},
		binary: binary,
	}, nil
}

func validateInventory(components releaseComponents) error {
	seen := map[string]bool{manifestName: true, attestationName: true, signatureName: true, publisherName: true}
	check := func(asset releaseArtifact) bool {
		name := strings.ToLower(asset.Name)
		if seen[name] || asset.ByteLength > maxArtifact {
			return false
		}
		seen[name] = true
		return true
	}
	for _, asset := range components.Backend.Platforms {
		if !check(asset) {
			return errors.New("invalid storage driver artifact inventory")
		}
	}
	if components.Web != nil && !check(*components.Web) {
		return errors.New("invalid storage driver Web artifact inventory")
	}
	return nil
}

func decodeReleaseDocument(raw []byte, schema *jsonschema.Schema, output any) bool {
	if len(raw) > maxDocument || !utf8.Valid(raw) {
		return false
	}
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.UseNumber()
	value, err := readUniqueJSON(reader, 0)
	if err != nil {
		return false
	}
	_, trailing := reader.Token()
	return trailing == io.EOF && schema.Validate(value) == nil && json.Unmarshal(raw, output) == nil
}

func readUniqueJSON(reader *json.Decoder, depth int) (any, error) {
	if depth >= 128 {
		return nil, errors.New("release document is nested too deeply")
	}
	token, err := reader.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		return readUniqueObject(reader, depth)
	case json.Delim('['):
		list := make([]any, 0)
		for reader.More() {
			value, err := readUniqueJSON(reader, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		_, err := reader.Token()
		return list, err
	default:
		return token, nil
	}
}

func readUniqueObject(reader *json.Decoder, depth int) (map[string]any, error) {
	result := make(map[string]any)
	for reader.More() {
		name, err := reader.Token()
		if err != nil {
			return nil, err
		}
		key, ok := name.(string)
		if _, duplicate := result[key]; !ok || duplicate {
			return nil, errors.New("ambiguous release document member")
		}
		value, err := readUniqueJSON(reader, depth+1)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	_, err := reader.Token()
	return result, err
}
