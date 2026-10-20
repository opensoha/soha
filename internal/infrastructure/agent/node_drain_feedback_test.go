package agent

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDrainResponseOnlyAcceptsKnownSafeCodesOnDrainPath(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		typed            bool
	}{
		{"pdb", basicMutationPath + "/infrastructure/nodes/node/drain", `{"error":{"code":"node_drain_pdb_blocked","message":"private token"}}`, true},
		{"unknown", basicMutationPath + "/infrastructure/nodes/node/drain", `{"error":{"code":"arbitrary","message":"private token"}}`, false},
		{"other operation", basicMutationPath + "/namespaces/team", `{"error":{"code":"node_drain_pdb_blocked"}}`, false},
		{"oversize", basicMutationPath + "/infrastructure/nodes/node/drain", strings.Repeat(" ", 8193) + `{"error":{"code":"node_drain_pdb_blocked"}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := agentResponseError(test.path, &http.Response{StatusCode: 409, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(test.body))})
			var failure *cr.NodeDrainError
			if errors.As(err, &failure) != test.typed || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe or missing classification: %v", err)
			}
			if !test.typed && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("original conflict mapping changed: %v", err)
			}
		})
	}
}
