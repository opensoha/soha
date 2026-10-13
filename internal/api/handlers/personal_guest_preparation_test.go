package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
)

type guestPreparationStub struct{ calls int }

func (s *guestPreparationStub) Prepare(context.Context, identity.Principal, sohaapi.PersonalGuestPreparationInput) (sohaapi.PersonalGuestPreparation, error) {
	s.calls++
	return sohaapi.PersonalGuestPreparation{}, nil
}

func TestPersonalGuestPreparationHTTPBoundsAndDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"environmentVersion":1,"csrPEM":"public"}`, `{"environmentVersion":1,"environmentVersion":2,"csrPEM":"public"}`, `{"environmentVersion":1,"csrPEM":"public","ownerId":"foreign"}`, `{"environmentVersion":9007199254740992,"csrPEM":"public"}`, `{"environmentVersion":1,"csrPEM":"` + strings.Repeat("a", 8193) + `"}`} {
		stub := &guestPreparationStub{}
		router := gin.New()
		router.POST("/prepare", NewPersonalGuestPreparationHandler(stub).Prepare)
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/prepare", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		want := 400
		if body == `{"environmentVersion":1,"csrPEM":"public"}` {
			want = 201
		}
		if response.Code != want || response.Header().Get("Cache-Control") != "no-store" || (want == 400 && stub.calls != 0) {
			t.Fatal("bootstrap HTTP boundary or cache policy failed", response.Code)
		}
	}
	var disabled *guestPreparationStub
	router := gin.New()
	router.POST("/prepare", NewPersonalGuestPreparationHandler(disabled).Prepare)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/prepare", strings.NewReader(`{}`)))
	if response.Code != 503 {
		t.Fatal("disabled preparation did not fail closed")
	}
}
