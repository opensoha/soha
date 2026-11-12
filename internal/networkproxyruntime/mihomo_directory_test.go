package networkproxyruntime

import (
	"reflect"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestMihomoOwnedDirectoryArguments(t *testing.T) {
	adapter := &clashEngineAdapter{config: Config{Engine: domain.EngineMihomo}}
	for _, tc := range []struct {
		path  string
		check bool
		want  []string
	}{
		{"/private state/.candidate-123", true, []string{"-d", "/private state", "-t", "-f", "/private state/.candidate-123"}},
		{"/private state/engine.conf", false, []string{"-d", "/private state", "-f", "/private state/engine.conf"}},
	} {
		if got := adapter.commandArgs(tc.path, tc.check); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("args = %q, want %q", got, tc.want)
		}
	}
}
