package pluginactivation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
	"github.com/opensoha/soha/internal/application/systemintegration"
	"github.com/opensoha/soha/internal/infrastructure/storagedriverhost"
	"github.com/stretchr/testify/require"
)

const streamReleaseMarker = "stream-release"

// The same signed fixture binary reports v1 on the first (odd) account and
// v2 on the second (even) account. B7 launches both through its real PID guard.
func TestMain(m *testing.M) {
	switch {
	case storagedriverhost.IsSandboxChild():
		os.Exit(storagedriverhost.RunSandboxChild())
	case os.Args[0] == "soha-storage-driver":
		version := "2.0.0"
		if os.Getuid()%2 == 1 {
			version = "1.0.0"
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		err := sdk.Serve(ctx, os.Stdin, sdk.ServerOptions{PluginID: fixturePlugin, Version: version}, nativeFixtureDriver{version: version})
		stop()
		if err != nil {
			os.Exit(1)
		}
	default:
		os.Exit(m.Run())
	}
}

func TestLinuxSwitchSignedStorageBackends(t *testing.T) {
	root := os.Getenv("SOHA_STORAGE_DRIVER_TEST_ROOT")
	if root == "" {
		t.Skip("requires a trusted Linux runtime root and two dedicated test accounts")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	accounts := [2]Account{nativeFixtureAccount(t, ""), nativeFixtureAccount(t, "_SECOND")}
	require.Equal(t, uint32(1), accounts[0].UID%2, "first test UID must be odd")
	require.Equal(t, uint32(0), accounts[1].UID%2, "second test UID must be even")
	base, err := os.MkdirTemp(root, "v17-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(base)) })
	require.NoError(t, os.Chmod(base, 0o711))
	cfg := Config{
		PackageRoot: filepath.Join(base, "packages"), RuntimeRoot: filepath.Join(base, "run"),
		Slots: map[string][2]Account{fixturePlugin: accounts},
	}
	require.NoError(t, os.Mkdir(cfg.RuntimeRoot, 0o700))
	require.NoError(t, os.Chmod(cfg.RuntimeRoot, 0o711))
	sign := nativeFixturePackages(t, base, cfg.PackageRoot)
	v1, v2, bad := sign("1.0.0", "a"), sign("2.0.0", "b"), sign("3.0.0", "c")

	m, err := New(ctx, cfg, systemintegration.NewStorageDriverRegistry(nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	committed := ""
	switchTo := func(release Release) error {
		return m.Switch(ctx, release, func() error {
			committed = release.Version
			return nil
		})
	}
	require.NoError(t, switchTo(v1))
	require.Error(t, switchTo(bad), "candidate reports a different authenticated identity")
	require.Equal(t, v1.Version, committed)
	require.True(t, m.Ready(fixturePlugin, v1.Version))

	held := nativeOpen(t, ctx, m, "held")
	defer held.Close()
	require.NoError(t, switchTo(v2))
	require.Equal(t, v2.Version, committed)
	require.True(t, m.Ready(fixturePlugin, v2.Version))
	require.Equal(t, v2.Version, nativeRead(t, nativeOpen(t, ctx, m, "now")))
	// Keep the v1 stream open beyond B7's normal five-second stop grace.
	timer := time.NewTimer(5500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	pattern := filepath.Join(cfg.RuntimeRoot, fmt.Sprintf("s-%d", accounts[0].UID), "d-*", "w")
	work, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.Len(t, work, 1, "old process work directory must remain until the stream drains")
	require.NoError(t, os.WriteFile(filepath.Join(work[0], streamReleaseMarker), nil, 0o644))
	require.Equal(t, v1.Version, nativeRead(t, held))

	require.NoError(t, switchTo(v1))
	require.Equal(t, v1.Version, committed)
	require.True(t, m.Ready(fixturePlugin, v1.Version))
	require.Equal(t, v1.Version, nativeRead(t, nativeOpen(t, ctx, m, "now")))
	require.NoError(t, m.Disable(ctx, fixturePlugin, func() error { return nil }))
	require.NoError(t, m.WaitRetired(ctx, fixturePlugin))
	remaining, err := filepath.Glob(filepath.Join(cfg.RuntimeRoot, "s-*"))
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func nativeFixtureAccount(t *testing.T, suffix string) Account {
	t.Helper()
	read := func(field string) uint32 {
		name := "SOHA_STORAGE_DRIVER_TEST" + suffix + "_" + field
		value, err := strconv.ParseUint(os.Getenv(name), 10, 32)
		require.NoError(t, err, name)
		return uint32(value)
	}
	return Account{UID: read("UID"), GID: read("GID")}
}

func nativeFixturePackages(t *testing.T, base, packageRoot string) func(string, string) Release {
	t.Helper()
	binary, err := os.ReadFile("/proc/self/exe")
	require.NoError(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(public)
	require.NoError(t, err)
	key := filepath.Join(base, "publisher.pem")
	require.NoError(t, os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600))
	const artifact = "fixture-driver"
	inventory := map[string]any{"backend": map[string]any{
		"driverId": fixtureDriver, "protocol": "object-storage/v1",
		"platforms": map[string]any{runtime.GOOS + "/" + runtime.GOARCH: map[string]any{
			"name": artifact, "byteLength": len(binary), "sha256": nativeSHA256(binary),
		}},
	}}
	return func(version, commitCharacter string) Release {
		release := fixtureRelease(version)
		release.Repository, release.Commit, release.PublicKeyPath = "https://github.com/example/storage", strings.Repeat(commitCharacter, 40), key
		manifest, err := json.Marshal(map[string]any{
			"id": release.PluginID, "name": "Native Blob", "publisher": "example", "version": version, "type": "resource-extension",
			"runtime": map[string]string{"mode": "executable-package-v1"}, "components": inventory,
			"configSchema": (&fixtureSupervisor{}).Descriptors()[0].ConfigSchema,
		})
		require.NoError(t, err)
		attestation, err := json.Marshal(map[string]any{
			"schemaVersion": "1", "repository": release.Repository, "commitSha": release.Commit,
			"pluginId": release.PluginID, "version": version, "rawManifestSha256": nativeSHA256(manifest), "components": inventory,
		})
		require.NoError(t, err)
		dir := filepath.Join(packageRoot, release.PluginID, version)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		for name, content := range map[string][]byte{
			"plugin.manifest.json": manifest, "soha-plugin-attestation.json": attestation,
			"soha-plugin-attestation.sig": ed25519.Sign(private, attestation), artifact: binary,
		} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o600))
		}
		return release
	}
}

func nativeSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func nativeOpen(t *testing.T, ctx context.Context, m *Manager, key string) io.ReadCloser {
	t.Helper()
	selected := m.current(fixturePlugin)
	require.NotNil(t, selected.backend)
	require.Equal(t, selected.release.Version, m.slots[fixturePlugin].owner.ActiveVersion(fixtureDriver))
	driver, err := selected.backend.host.Driver(fixtureDriver)
	require.NoError(t, err)
	opened, err := driver.OpenObject(ctx, storagedriver.OpenObjectRequest{
		Binding: storagedriver.Binding{ID: "fixture-binding", Container: "fixture"}, Key: key,
	})
	require.NoError(t, err)
	return opened.Body
}

func nativeRead(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	defer body.Close()
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(content)
}

type nativeFixtureDriver struct {
	storagedriver.Driver
	version string
}

func (d nativeFixtureDriver) OpenObject(ctx context.Context, request storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	var content io.Reader = strings.NewReader(d.version)
	if request.Key == "held" {
		content = gatedStream{ctx: ctx, content: content}
	}
	return storagedriver.OpenObjectResult{
		ETag: `"fixture"`, ContentType: "application/octet-stream", ContentLength: int64(len(d.version)), Body: io.NopCloser(content),
	}, nil
}

type gatedStream struct {
	ctx     context.Context
	content io.Reader
}

func (s gatedStream) Read(p []byte) (int, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := os.Stat(streamReleaseMarker)
		if err == nil {
			return s.content.Read(p)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		select {
		case <-s.ctx.Done():
			return 0, s.ctx.Err()
		case <-ticker.C:
		}
	}
}
