package networkproxyruntime

import (
	"reflect"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestV2RayCommandDeclaresJSONForExtensionlessFiles(t *testing.T) {
	engine := &engine{config: Config{Engine: domain.EngineV2Ray, EngineBinary: "v2ray"}}
	for _, test := range []struct {
		check bool
		want  []string
	}{
		{check: true, want: []string{"v2ray", "test", "-format=json", "-config", "/state/engine.conf"}},
		{check: false, want: []string{"v2ray", "run", "-format=json", "-config", "/state/engine.conf"}},
	} {
		if got := engine.command("/state/engine.conf", test.check).Args; !reflect.DeepEqual(got, test.want) {
			t.Fatalf("command args = %v; want %v", got, test.want)
		}
	}
}

func TestV2RayTotalsExcludesManagementTraffic(t *testing.T) {
	upload, download, err := v2rayTotals([]byte(`{"stat":[
		{"name":"inbound>>>proxy>>>traffic>>>uplink","value":"123"},
		{"name":"inbound>>>proxy>>>traffic>>>downlink","value":"456"},
		{"name":"inbound>>>soha-api>>>traffic>>>uplink","value":"222"},
		{"name":"inbound>>>soha-api>>>traffic>>>downlink","value":"54"}
	]}`))
	if err != nil || upload != 123 || download != 456 {
		t.Fatalf("v2rayTotals = %d, %d, %v; want 123, 456", upload, download, err)
	}
}

func TestV2RayTotalsRejectsInvalidCounter(t *testing.T) {
	if _, _, err := v2rayTotals([]byte(`{"stat":[{"name":"inbound>>>proxy>>>traffic>>>uplink","value":"-1"}]}`)); err == nil {
		t.Fatal("negative counter was accepted")
	}
}
