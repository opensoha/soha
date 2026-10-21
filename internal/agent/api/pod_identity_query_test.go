package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPodExpectedUIDDoesNotDowngradeMalformedInput(t *testing.T) {
	for _, test := range []struct {
		query  string
		status int
		uid    string
	}{
		{"", 200, ""}, {"?expectedUid=original", 200, "original"},
		{"?expectedUid=", 400, ""}, {"?expectedUid=%20", 400, ""}, {"?expectedUid=%XX", 400, ""},
		{"?expectedUid=&expectedUid=old", 400, ""}, {"?expectedUid=old&expectedUid=other", 400, ""},
	} {
		t.Run(test.query, func(t *testing.T) {
			router := gin.New()
			router.GET("/session", func(c *gin.Context) {
				uid, valid := podExpectedUID(c)
				if !valid {
					return
				}
				c.String(http.StatusOK, uid)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/session"+test.query, nil))
			if response.Code != test.status || (test.status == 200 && response.Body.String() != test.uid) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}
