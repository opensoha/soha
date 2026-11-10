package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestMonitoringQueryRejectsRawSelectorsAndNamespaceFilters(t *testing.T) {
	for _, query := range []string{"namespace=team", "query=up", "view=cpu&view=memory", "stepSeconds=bad"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/?"+query, nil)
		if _, err := parseResourceMonitoringQuery(context); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/?view=cpu&node=n&rangeMinutes=60", nil)
	input, err := parseResourceMonitoringQuery(context)
	if err != nil || input.Node != "n" || input.RangeMinutes != 60 {
		t.Fatalf("valid query rejected: %+v %v", input, err)
	}
}
