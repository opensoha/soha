package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoplugin "github.com/opensoha/soha/internal/repository/plugin"
	"go.uber.org/zap"
)

type distributionRoutePermissions struct{}

func (distributionRoutePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"installer": {appaccess.PermPluginInstall, appaccess.PermPluginUpgrade, appaccess.PermPluginLifecycle, appaccess.PermPluginView, appaccess.PermPlatformExtensionsView},
		"viewer":    {appaccess.PermPluginView},
	}, nil
}

type distributionRouteSource struct {
	manifest, nextManifest []byte
}

const distributionFixtureURL = "https://github.com/example/soha-skill-pack"
const distributionFixtureSHA = "0123456789abcdef0123456789abcdef01234567"
const distributionNextSHA = "89abcdef0123456789abcdef0123456789abcdef"

func (s distributionRouteSource) Resolve(_ context.Context, source, ref string) (string, string, string, error) {
	if source != distributionFixtureURL {
		return "", "", "", apperrors.ErrNotFound
	}
	if ref == "v0.2.0" {
		return source, ref, distributionNextSHA, nil
	}
	return source, ref, distributionFixtureSHA, nil
}

func (s distributionRouteSource) FetchManifest(_ context.Context, source, sha string) ([]byte, error) {
	if source == distributionFixtureURL {
		switch sha {
		case distributionFixtureSHA:
			return s.manifest, nil
		case distributionNextSHA:
			return s.nextManifest, nil
		}
	}
	return nil, apperrors.ErrNotFound
}

func TestRepositoryDistributionRoutesWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("requires an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFromFile(t.Context(), "../../../migrations/postgres"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../contracts/examples/plugin-distribution/plugin.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	repo := repoplugin.New(store.DB())
	nextRaw := bytes.Replace(raw, []byte(`"version": "0.1.0"`),
		[]byte(`"version": "0.2.0", "permissions": {"required": ["plugin.view"]}`), 1)
	service := appplugin.NewWithOptions(repo, appaccess.NewPermissionResolver(distributionRoutePermissions{}), nil,
		appplugin.WithRepositoryDistribution(distributionRouteSource{manifest: raw, nextManifest: nextRaw}, bytes.Repeat([]byte{42}, 32)))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// Exercise the normal route and permission resolver with principals from the local test.
	router.Use(func(c *gin.Context) {
		role := c.GetHeader("X-Fixture-Role")
		if c.GetHeader("Authorization") == "Bearer task136-local-fixture" {
			role = "installer"
		}
		c.Set("principal", domainidentity.Principal{UserID: role, Roles: []string{role}})
	})
	group := router.Group("/api/v1", apiMiddleware.RequireAuth())
	registerPluginRoutes(group, Dependencies{Plugins: apiHandlers.NewPluginHandler(service)})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	previewPath := "/plugins/repository/preview"
	previewBody := `{"repositoryUrl":"` + distributionFixtureURL + `"}`
	callDistributionRoute(t, server, "viewer", http.MethodPost, previewPath, previewBody, http.StatusForbidden)
	result := callDistributionRoute(t, server, "installer", http.MethodPost, previewPath, previewBody, http.StatusOK)
	var preview struct {
		Data domainplugin.PluginRepositoryPreview
	}
	if err := json.Unmarshal(result, &preview); err != nil || preview.Data.PreviewToken == "" || preview.Data.SourceInfo.CommitSha != distributionFixtureSHA {
		t.Fatalf("invalid repository preview evidence: %v", err)
	}
	id := preview.Data.Manifest.ID
	before := callDistributionRoute(t, server, "installer", http.MethodGet, "/plugins/installed", "", http.StatusOK)
	if bytes.Contains(before, []byte(id)) {
		t.Fatal("preview created an installation")
	}
	input, err := json.Marshal(domainplugin.PluginRepositoryInstallRequest{PreviewToken: preview.Data.PreviewToken})
	if err != nil {
		t.Fatal(err)
	}
	installPath := "/plugins/repository/install"
	if cliBinary := os.Getenv("SOHA_PLUGIN_TEST_CLI"); cliBinary != "" {
		t.Run("cli_binary", func(t *testing.T) {
			testDistributionCLI(t, cliBinary, server.URL, id, preview.Data.SourceInfo.RawManifestChecksum)
		})
	}
	callDistributionRoute(t, server, "installer", http.MethodPost, installPath, string(input), http.StatusCreated)
	callDistributionRoute(t, server, "installer", http.MethodPost, "/plugins/"+id+"/enable", "", http.StatusOK)
	callDistributionRoute(t, server, "installer", http.MethodPost, installPath, string(input), http.StatusCreated)
	item, err := repo.GetInstalled(t.Context(), id)
	if err != nil || item.Status != "enabled" || item.SourceInfo == nil || item.SourceInfo.CommitSha != distributionFixtureSHA {
		t.Fatalf("readback did not preserve install source and enabled state: status=%s err=%v", item.Status, err)
	}
	extensions := callDistributionRoute(t, server, "installer", http.MethodGet, "/extensions/ai", "", http.StatusOK)
	if !bytes.Contains(extensions, []byte(id)) {
		t.Fatal("enabled repository plugin did not register its declaration")
	}
	callDistributionRoute(t, server, "installer", http.MethodPost, "/plugins/"+id+"/upgrade", `{}`, http.StatusConflict)
	callDistributionRoute(t, server, "installer", http.MethodPost, "/plugins/install", string(input), http.StatusBadRequest)
	manifestInput, err := json.Marshal(domainplugin.PluginInstallRequest{Manifest: &preview.Data.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	callDistributionRoute(t, server, "installer", http.MethodPost, "/plugins/install", string(manifestInput), http.StatusConflict)

	next := callDistributionRoute(t, server, "installer", http.MethodPost, previewPath,
		`{"repositoryUrl":"`+distributionFixtureURL+`","ref":"v0.2.0"}`, http.StatusOK)
	if err := json.Unmarshal(next, &preview); err != nil || preview.Data.SourceInfo.CommitSha != distributionNextSHA {
		t.Fatalf("invalid upgrade preview: %v", err)
	}
	upgradePath := "/plugins/" + id + "/repository/upgrade"
	unconfirmed := `{"previewToken":"` + preview.Data.PreviewToken + `"}`
	callDistributionRoute(t, server, "viewer", http.MethodPost, upgradePath, unconfirmed, http.StatusForbidden)
	rejected := callDistributionRoute(t, server, "installer", http.MethodPost, upgradePath, unconfirmed, http.StatusConflict)
	if !bytes.Contains(rejected, []byte("plugin_permission_increase")) {
		t.Fatal("the upgrade did not require explicit permission acknowledgement")
	}
	confirmed := `{"previewToken":"` + preview.Data.PreviewToken + `","confirmPermissionIncrease":true}`
	callDistributionRoute(t, server, "installer", http.MethodPost, upgradePath, confirmed, http.StatusOK)
	item, err = repo.GetInstalled(t.Context(), id)
	if err != nil || item.Version != "0.2.0" || item.Status != "enabled" ||
		item.SourceInfo == nil || item.SourceInfo.CommitSha != distributionNextSHA {
		t.Fatalf("upgraded readback: version=%s status=%s err=%v", item.Version, item.Status, err)
	}
}

func testDistributionCLI(t *testing.T, cliBinary, server, id, checksum string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), cliBinary, "plugin", "install", "--profile", "local",
		"--repository", distributionFixtureURL, "--yes", "--json")
	command.Env = append(os.Environ(), "SOHA_SERVER="+server, "SOHA_TOKEN=task136-local-fixture",
		"SOHA_CONFIG="+filepath.Join(t.TempDir(), "config.json"))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("repository install with CLI failed: %v (%s)", err, stderr.String())
	}
	var item domainplugin.InstalledPlugin
	if err := json.Unmarshal(output, &item); err != nil || item.ID != id || item.Status != "disabled" {
		t.Fatalf("invalid CLI install result: id=%s status=%s err=%v", item.ID, item.Status, err)
	}
	if !strings.Contains(stderr.String(), distributionFixtureSHA) || !strings.Contains(stderr.String(), checksum) ||
		strings.Contains(stderr.String(), "previewToken") || bytes.Contains(output, []byte("previewToken")) {
		t.Fatal("CLI preview did not safely display pinned evidence")
	}
}

func callDistributionRoute(t *testing.T, server *httptest.Server, role, method, path, body string, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fixture-Role", role)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var data json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil || response.StatusCode != want {
		t.Fatalf("%s %s returned status %d (want %d), decode err: %v", method, path, response.StatusCode, want, err)
	}
	return data
}
