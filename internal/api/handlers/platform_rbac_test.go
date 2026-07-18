package handlers

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestServiceAccountSubjectFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		query               string
		wantNamespace       string
		wantName            string
		wantRequested       bool
		wantInvalidArgument bool
	}{
		{name: "absent"},
		{name: "complete", query: "subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a", wantNamespace: "team-a", wantName: "builder", wantRequested: true},
		{name: "partial", query: "subjectKind=ServiceAccount&subjectName=builder", wantRequested: true, wantInvalidArgument: true},
		{name: "unsupported kind", query: "subjectKind=User&subjectName=builder&subjectNamespace=team-a", wantRequested: true, wantInvalidArgument: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("GET", "/?"+tt.query, nil)
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = request

			namespace, name, requested, err := serviceAccountSubjectFilter(context)
			if namespace != tt.wantNamespace || name != tt.wantName || requested != tt.wantRequested {
				t.Fatalf("serviceAccountSubjectFilter() = (%q, %q, %v), want (%q, %q, %v)", namespace, name, requested, tt.wantNamespace, tt.wantName, tt.wantRequested)
			}
			if errors.Is(err, apperrors.ErrInvalidArgument) != tt.wantInvalidArgument {
				t.Fatalf("serviceAccountSubjectFilter() error = %v, invalid argument = %v", err, tt.wantInvalidArgument)
			}
		})
	}
}
