package manifestruntime

import (
	"context"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGitCheckoutRejectsCommitOptionBeforeExecution(t *testing.T) {
	for _, commit := range []string{"--upload-pack=untrusted", "HEAD", "-c", "abc"} {
		t.Run(commit, func(t *testing.T) {
			_, _, err := checkoutManifestRepository(context.Background(), domainmanifest.TaskPayload{RequestedCommit: commit}, t.TempDir()+"/not-created", nil)
			require.ErrorContains(t, err, "full commit hash")
		})
	}
}
