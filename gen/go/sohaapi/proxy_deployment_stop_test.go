package sohaapi

import (
	"encoding/json"
	"testing"
)

func TestProxyDeploymentStopFlagsPreserveKnownZero(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		raw, err := json.Marshal(NetworkProxyDeploymentRuntimeAck{ProcessStopped: stopped, ActiveCountKnown: true, ActiveConnections: 0, AcceptingRequests: false})
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if string(value["activeConnections"]) != "0" {
			t.Fatalf("known zero lost: %s", raw)
		}
		flag, present := value["processStopped"]
		if present != stopped || stopped && string(flag) != "true" {
			t.Fatalf("stopped flag presence: %s", raw)
		}
	}
	for _, required := range []bool{false, true} {
		raw, err := json.Marshal(NetworkProxyDeploymentRuntimeControl{StopRequired: required})
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		flag, present := value["stopRequired"]
		if present != required || required && string(flag) != "true" {
			t.Fatalf("stop flag presence: %s", raw)
		}
	}
}
