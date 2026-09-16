package providerportal

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appidentityprovider "github.com/opensoha/soha/internal/application/identityprovider"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	providerrepo "github.com/opensoha/soha/internal/repository/identityprovider"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Run against a disposable PostgreSQL database; migrations are applied to it.
func TestOutpostManagementHTTPWithPostgres(t *testing.T) {
	db := ssoTestDatabase(t)
	ctx := context.Background()
	repo := providerrepo.New(db)
	service := appidentityprovider.New(repo, nil, appaccess.NewPermissionResolver(outpostTestPermissions{}), nil, "test-encryption-key-32-bytes-long")
	seed := sha256.Sum256([]byte("outpost-http-test"))
	service.SetOutpostSigningKey("test-key", ed25519.NewKeyFromSeed(seed[:]))
	handler := New(Services{Outposts: service})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "test-admin", Roles: []string{"admin"}})
	})
	router.POST("/outposts", handler.CreateOutpost)
	router.GET("/outposts", handler.ListOutposts)
	router.GET("/outposts/:outpostID", handler.GetOutpost)
	router.PUT("/outposts/:outpostID", handler.UpdateOutpost)
	router.POST("/outposts/:outpostID/rotate", handler.RotateOutpostToken)
	input := domainprovider.OutpostInput{Name: "Integration edge", Mode: "external", Status: "online", Version: "forged", Metadata: map[string]any{"owner": "admin"}}
	created := requestOutpost(t, router, "POST", "/outposts", input, http.StatusCreated)
	defer func() { _ = repo.DeleteOutpost(ctx, created.ID) }()
	ssoCheck(t, created.ConfigurationVersion == 0 && created.RuntimeReason == "awaiting_registration" && created.Status == "offline", "created = %#v", created)
	config, err := service.ClaimIdentityOutpostRuntime(ctx, created.Token, sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1", RuntimeVersion: "test-agent"})
	ssoNoError(t, err)
	if _, err := service.HeartbeatIdentityOutpostRuntime(ctx, created.ID, created.Token, sohaapi.IdentityOutpostHeartbeatRequest{AgentID: created.ID, ConfigurationVersion: config.ConfigurationVersion, ConfigurationExpiresAt: &config.ExpiresAt, Status: sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy}); err != nil {
		t.Fatal(err)
	}
	view := requestOutpost(t, router, "GET", "/outposts/"+created.ID, nil, http.StatusOK)
	ssoCheck(t, view.RuntimeStatus == "available" && view.ConfigurationVersion == config.ConfigurationVersion && view.LastHeartbeatAt != nil, "runtime view = %#v", view)
	staleEdit, err := repo.GetOutpost(ctx, created.ID)
	ssoNoError(t, err)
	input.Name = "Edited edge"
	view = requestOutpost(t, router, "PUT", "/outposts/"+created.ID, input, http.StatusOK)
	ssoCheck(t, view.RuntimeStatus == "available" && view.RuntimeVersion == "test-agent", "management erased runtime = %#v", view)
	rotated := requestOutpost(t, router, "POST", "/outposts/"+created.ID+"/rotate", nil, http.StatusOK)
	ssoCheck(t, rotated.LastHeartbeatAt == nil && rotated.RuntimeReason == "awaiting_registration", "rotated = %#v", rotated)
	if _, err := repo.RecordOutpostClaim(ctx, staleEdit); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("old in-flight claim = %v", err)
	}
	if _, err := repo.RecordOutpostHeartbeat(ctx, staleEdit); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("old in-flight heartbeat = %v", err)
	}
	staleEdit.Name = "Late admin edit"
	if _, err := repo.UpdateOutpost(ctx, staleEdit); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.GetOutpost(ctx, created.ID)
	ssoCheck(t, err == nil && persisted.TokenHash != staleEdit.TokenHash && persisted.LastHeartbeatAt == nil, "stale admin update restored token/runtime: %v", err)
	requestOutpost(t, router, "GET", "/outposts/"+created.ID, nil, http.StatusOK)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/outposts", nil))
	ssoCheck(t, recorder.Code == http.StatusOK, "list status = %d", recorder.Code)
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	ssoNoError(t, json.Unmarshal(recorder.Body.Bytes(), &list))
	ssoCheck(t, len(list.Items) != 0, "empty outpost list")
	for _, raw := range list.Items {
		validateOutpostResponse(t, raw)
	}
}

type outpostTestPermissions struct{}

func (outpostTestPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"admin": {appaccess.PermIdentityOutpostsView, appaccess.PermIdentityOutpostsManage}}, nil
}

func requestOutpost(t *testing.T, router http.Handler, method, path string, input any, status int) domainprovider.Outpost {
	t.Helper()
	body, err := json.Marshal(input)
	ssoNoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	ssoCheck(t, recorder.Code == status, "%s %s status = %d: %s", method, path, recorder.Code, recorder.Body.String())
	var response struct {
		Item json.RawMessage `json:"data"`
	}
	ssoNoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	validateOutpostResponse(t, response.Item)
	var outpost domainprovider.Outpost
	ssoNoError(t, json.Unmarshal(response.Item, &outpost))
	return outpost
}

func validateOutpostResponse(t *testing.T, raw []byte) {
	t.Helper()
	validateSSOResponse(t, "IdentityOutpost", raw)
}

func validateSSOResponse(t *testing.T, schemaName string, raw []byte) {
	t.Helper()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contractsopenapi.JSON()))
	ssoNoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	const location = "https://contracts.opensoha.dev/openapi.json"
	ssoNoError(t, compiler.AddResource(location, document))
	schema, err := compiler.Compile(location + "#/components/schemas/" + schemaName)
	ssoNoError(t, err)
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	ssoNoError(t, err)
	if err := schema.Validate(value); err != nil {
		t.Fatalf("actual handler response violates %s: %v", schemaName, err)
	}
}

func ssoTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	portText := os.Getenv("SOHA_SSO_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_SSO_TEST_POSTGRES_PORT to a disposable PostgreSQL database")
	}
	port, err := strconv.Atoi(portText)
	ssoNoError(t, err)
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute}, zap.NewNop())
	ssoNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ssoNoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "..", "migrations", "postgres")))

	return store.DB()
}
