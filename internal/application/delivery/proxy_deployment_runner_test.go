package delivery

import "testing"

func TestGenericRunnerCannotReadOrCallbackPrivateProxyDeploymentTask(t *testing.T) {
	assertGenericRunnerDeniesPrivate(t, "proxy_deployment.edge")
}
