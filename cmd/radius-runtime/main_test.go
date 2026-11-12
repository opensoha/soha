package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	runtime "github.com/opensoha/soha/internal/radiusruntime"
)

func TestDecodeApprovedConfigurationRejectsUnknownAndMissingReferences(t *testing.T) {
	for _, data := range []string{`{"unexpected":true}`, `{} {}`, `{"installation":{},"control":{}}`} {
		if _, err := decodeConfiguration(strings.NewReader(data)); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	c := configuration{Installation: runtime.Installation{ConfigurationSHA256: strings.Repeat("a", 64), Materials: map[string]string{"/cert": strings.Repeat("b", 64), "/key": strings.Repeat("c", 64), "/ca": strings.Repeat("d", 64)}}}
	c.Control.CertificateFile = "/cert"
	c.Control.KeyFile = "/key"
	c.Control.CAFile = "/ca"
	c.Control.Configuration.TreeHash = "sha256:" + strings.Repeat("a", 64)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeConfiguration(strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationDigestCommandIsExclusiveAndReadOnly(t *testing.T) {
	if _, err := parseCommandOptions([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatal("help was not preserved", err)
	}
	options, err := parseCommandOptions([]string{"--configuration-digest", "/etc/radius/snapshot-A"})
	if err != nil || options.digest != "/etc/radius/snapshot-A" {
		t.Fatal(options, err)
	}
	for _, args := range [][]string{{"--config", "/etc/runtime.json", "--configuration-digest", "/etc/radius"}, {"--unknown"}, {"positional"}, {"--configuration-digest", ""}} {
		if _, err := parseCommandOptions(args); err == nil {
			t.Fatal("invalid flags accepted", args)
		}
	}
	// A replaceable temp ancestor is rejected before configuration loading,
	// control networking or child execution; the command prints no partial hash.
	var output strings.Builder
	if err := executeCommand(context.Background(), commandOptions{digest: t.TempDir(), config: "/must-not-be-read"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("unprotected digest accepted", err)
	}
}

func catalogConfigurationFixture() configuration {
	common := map[string]string{"/etc/radius/control.crt": strings.Repeat("a", 64), "/etc/radius/control.key": strings.Repeat("b", 64), "/etc/radius/ca.crt": strings.Repeat("c", 64)}
	c := configuration{Installation: runtime.Installation{Socket: "/run/soha-radius/hook.sock", Materials: common}}
	c.Control = runtime.ClientConfiguration{RuntimeID: "runtime-1", CertificateFile: "/etc/radius/control.crt", KeyFile: "/etc/radius/control.key", CAFile: "/etc/radius/ca.crt"}
	for _, name := range []string{"A", "B"} {
		hash := strings.Repeat(strings.ToLower(name), 64)
		entry := runtime.ConfigurationSnapshot{Material: api.NetworkRADIUSConfiguration{Reference: "snapshot-" + name, TreeHash: "sha256:" + hash, HooksAttested: true, EapCARef: "ca-ref", EapServerCertificateRef: "cert-ref", EapServerKeyRef: "key-ref", NasSharedSecretRef: "secret-ref"}, Directory: "/etc/radius/" + name, TreeSHA256: hash, Hooks: runtime.HookAttestation{ConfigurationSHA256: hash, Socket: c.Installation.Socket, Routes: []string{"/admit", "/challenge", "/finish"}, ControlOnly: true, NonceAttribute: "control:Tmp-String-8", EpochEnvironment: "SOHA_RADIUS_PROCESS_EPOCH", SocketEnvironment: "SOHA_RADIUS_HOOK_SOCKET"}, Environment: map[string]string{"SOHA_RADIUS_RUNTIME_ID": "runtime-1", "SOHA_RADIUS_CONTROL_URL": "https://alternate-control.invalid", "SOHA_RADIUS_CONTROL_CERT_FILE": c.Control.CertificateFile, "SOHA_RADIUS_CONTROL_KEY_FILE": c.Control.KeyFile, "SOHA_RADIUS_CONTROL_CA_FILE": c.Control.CAFile}}
		c.Installation.ConfigurationCatalog = append(c.Installation.ConfigurationCatalog, entry)
	}
	return c
}

func TestDecodeCatalogDerivesOnlyLocalCompleteAllowlist(t *testing.T) {
	for _, kind := range []string{"valid", "legacy-control", "different-certificate", "missing-key", "runtime-identity", "http-url", "common-hash-override"} {
		t.Run(kind, func(t *testing.T) {
			c := catalogConfigurationFixture()
			switch kind {
			case "legacy-control":
				c.Control.Configuration = c.Installation.ConfigurationCatalog[0].Material
			case "different-certificate":
				c.Installation.ConfigurationCatalog[1].Environment["SOHA_RADIUS_CONTROL_CERT_FILE"] = "/etc/radius/other.crt"
			case "missing-key":
				delete(c.Installation.ConfigurationCatalog[1].Environment, "SOHA_RADIUS_CONTROL_KEY_FILE")
			case "runtime-identity":
				c.Installation.ConfigurationCatalog[1].Environment["SOHA_RADIUS_RUNTIME_ID"] = "other-runtime"
			case "http-url":
				c.Installation.ConfigurationCatalog[1].Environment["SOHA_RADIUS_CONTROL_URL"] = "http://control.invalid"
			case "common-hash-override":
				c.Installation.ConfigurationCatalog[1].Materials = map[string]string{c.Control.CertificateFile: strings.Repeat("f", 64)}
			}
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeConfiguration(strings.NewReader(string(data)))
			if (err == nil) != (kind == "valid") {
				t.Fatalf("unexpected catalog admission: %v", err)
			}
			if err == nil && (len(decoded.Control.AllowedConfigurations) != 2 || len(decoded.Control.ApprovedMaterials) != 3) {
				t.Fatal("local approvals were not derived")
			}
		})
	}
}
