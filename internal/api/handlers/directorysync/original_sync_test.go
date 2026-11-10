package directorysync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type originalSyncHandlerService struct {
	*serviceStub
	actor, credential, key, connection string
	calls                              int
}

func (s *originalSyncHandlerService) SyncOriginal(_ context.Context, connection, actor, credential, key string, _ appdirectorysync.ConnectorFactory) (domain.Run, error) {
	s.calls++
	s.actor, s.credential, s.key, s.connection = actor, credential, key, connection
	return domain.Run{ID: "143cbe75-a373-4ae6-a608-75bff78c2d53", ConnectionID: connection, Trigger: "manual", Status: domain.RunRunning, RequestCredentialID: "private-binding", IdempotencyKey: "private-stored-key", CancelRequested: true}, nil
}
func (s *originalSyncHandlerService) GetOriginalSync(ctx context.Context, connection, actor, credential, key string) (domain.Run, error) {
	return s.SyncOriginal(ctx, connection, actor, credential, key, nil)
}

func (s *originalSyncHandlerService) RequestOriginalCancellation(_ context.Context, id, actor, credential string) (domain.Run, error) {
	s.calls++
	s.actor, s.credential = actor, credential
	return domain.Run{ID: id, ConnectionID: "owned", Trigger: "manual", Status: domain.RunRunning, CancelRequested: true}, nil
}

func TestDirectoryOriginalCancellationReportsRequestedRatherThanStopped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, connection := range []string{"owned", "foreign"} {
		service := &originalSyncHandlerService{}
		repo := &repositoryStub{run: domain.Run{ID: "original", ConnectionID: "owned", Status: domain.RunRunning, RequestCredentialID: "private-binding"}}
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("principal", domainidentity.Principal{UserID: "actor", AccessTokenID: "trusted-token"})
			c.Next()
		})
		router.POST("/connections/:connectionID/runs/:runID/cancel", New(repo, service, nil, nil).Cancel)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", "/connections/"+connection+"/runs/original/cancel", nil))
		if connection == "foreign" {
			if recorder.Code != 404 || service.calls != 0 {
				t.Fatal("URL connection fence failed")
			}
		} else if recorder.Code != 200 || service.calls != 1 || service.credential != "token:trusted-token" || !strings.Contains(recorder.Body.String(), `"status":"running"`) || !strings.Contains(recorder.Body.String(), `"cancelRequested":true`) {
			t.Fatal("cancellation claimed stopped work or lost original credential", recorder.Code, recorder.Body)
		}
	}
}

func TestDirectoryOriginalRequestHandlerBindsTrustedSessionOrToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := "1d397734-d897-484d-9009-21924e904080"
	for _, session := range []string{"", "trusted-session"} {
		for _, method := range []string{"POST", "GET"} {
			t.Run(method+"/"+session, func(t *testing.T) {
				service := &originalSyncHandlerService{}
				handler := New(nil, service, nil, nil)
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("principal", domainidentity.Principal{UserID: "actor", AccessTokenID: "token-id"})
					c.Set("access_context", domainidentity.AccessContext{SessionID: session})
					c.Next()
				})
				router.POST("/connections/:connectionID/requests", handler.CreateSyncRequest)
				router.GET("/connections/:connectionID/requests/:idempotencyKey", handler.GetSyncRequest)
				path := "/connections/owned/requests"
				if method == "GET" {
					path += "/" + key
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(`{"idempotencyKey":"`+key+`"}`)))
				wanted := "token:token-id"
				if session != "" {
					wanted = "session:" + session
				}
				if recorder.Code < 200 || recorder.Code >= 300 || service.actor != "actor" || service.credential != wanted || service.key != key || service.connection != "owned" || service.calls != 1 {
					t.Fatalf("trusted binding missing: status=%d body=%s", recorder.Code, recorder.Body)
				}
				var envelope map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(recorder.Body.String(), "private-") || !strings.Contains(recorder.Body.String(), `"cancelRequested":true`) {
					t.Fatal("private receipt fields leaked or cancel flag lost")
				}
			})
		}
	}
}

func TestDirectoryOriginalRequestHandlerRejectsUntrustedOrMalformedInput(t *testing.T) {
	for _, test := range []struct{ body, token string }{
		{`{"idempotencyKey":"1d397734-d897-484d-9009-21924e904080","credential":"spoof"}`, "trusted"},
		{`{"idempotencyKey":"1d397734-d897-484d-9009-21924e904080"} {}`, "trusted"},
		{`{"idempotencyKey":"invalid"}`, "trusted"},
		{strings.Repeat("x", 4097), "trusted"},
		{`{"idempotencyKey":"1d397734-d897-484d-9009-21924e904080"}`, ""},
	} {
		service := &originalSyncHandlerService{}
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("principal", domainidentity.Principal{UserID: "actor", AccessTokenID: test.token})
			c.Next()
		})
		router.POST("/connections/:connectionID/requests", New(nil, service, nil, nil).CreateSyncRequest)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/connections/owned/requests", strings.NewReader(test.body)))
		if recorder.Code < 400 || service.calls != 0 {
			t.Fatal("untrusted body reached service", recorder.Code)
		}
	}
}
