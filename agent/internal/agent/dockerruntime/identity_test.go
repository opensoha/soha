package dockerruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
)

func TestContainerObservationRejectsReplacementAndForeignCoordinates(t *testing.T) {
	p, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	b := protocol.ContainerBinding{ProjectBinding: p, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}
	labels := p.Labels()
	labels["com.docker.compose.project"] = p.Namespace
	labels["com.docker.compose.service"] = "web"
	item := Container{ID: b.ContainerID, Labels: labels}
	raw, _ := json.Marshal(item)
	if _, err := parseContainer(raw, b); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{protocol.HostUIDLabel, protocol.ProjectUIDLabel, protocol.ProtocolLabel, "com.docker.compose.project", "com.docker.compose.service"} {
		previous := labels[key]
		labels[key] = "other"
		raw, _ := json.Marshal(item)
		if _, err := parseContainer(raw, b); err == nil {
			t.Fatal("foreign coordinate accepted", key)
		}
		labels[key] = previous
	}
	item.ID = strings.Repeat("b", 64)
	raw, _ = json.Marshal(item)
	if _, err := parseContainer(raw, b); err == nil {
		t.Fatal("replacement ID accepted")
	}
	if _, err := parseContainer([]byte(`null`), b); err == nil {
		t.Fatal("null inspect accepted")
	}
	buffer := &boundedOutput{limit: 3}
	if _, err := buffer.Write([]byte("four")); err == nil || buffer.Len() != 0 {
		t.Fatal("inspect limit bypassed")
	}
}

func TestProjectInventoryRequiresFullUniqueIDsAndServiceNames(t *testing.T) {
	p, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	line := strings.Repeat("a", 64) + " web\n"
	if items, err := parseProjectTargets([]byte(line), p); err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if items, err := parseProjectTargets(nil, p); err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	tooMany := ""
	for i := 1; i <= 257; i++ {
		tooMany += fmt.Sprintf("%064x web\n", i)
	}
	for _, raw := range []string{line + line, "short web\n", strings.Repeat("a", 64) + " --help\n", strings.Repeat("a", 64) + " web extra\n", tooMany} {
		if _, err := parseProjectTargets([]byte(raw), p); err == nil {
			t.Fatal("ambiguous inventory accepted")
		}
	}
}
