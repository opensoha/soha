package copilot

import (
	"errors"
	"testing"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDSHPreSubmissionRejectionClassifiesUnavailableRuntime(t *testing.T) {
	service := &Service{}
	_, err := service.queueNativeMessage(t.Context(), domainidentity.Principal{},
		domaincopilot.Session{}, domaincopilot.SessionMetadata{}, domaincopilot.WorkbenchSendMessageInput{}, "")
	if !errors.Is(err, ErrWorkbenchRequestNotAccepted) || !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("expected explicit pre-submission rejection, got %v", err)
	}
}
