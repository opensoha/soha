package delivery

import "testing"

func TestGenericRunnerCannotReadOrCallbackPrivateOutpostTask(t *testing.T) {
	assertGenericRunnerDeniesPrivate(t, "outpost_runtime.edge")
}
