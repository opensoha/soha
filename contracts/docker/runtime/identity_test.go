package runtime

import (
	"strings"
	"testing"
)

func TestProjectIdentityNamespaceAndContainerFence(t *testing.T) {
	a, err := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	if err != nil || a.Validate() != nil {
		t.Fatal(a, err)
	}
	b, _ := NewProjectBinding(a.HostUID, "33333333-3333-4333-8333-333333333333")
	if a.Namespace == b.Namespace {
		t.Fatal("recreated project reused namespace")
	}
	changedHost, _ := NewProjectBinding(b.ProjectUID, a.ProjectUID)
	if a.Namespace == changedHost.Namespace {
		t.Fatal("host identity ignored")
	}
	container := ContainerBinding{ProjectBinding: a, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}
	labels := a.Labels()
	labels["com.docker.compose.project"] = a.Namespace
	labels["com.docker.compose.service"] = "web"
	if !container.MatchesContainer(container.ContainerID, labels) {
		t.Fatal("matching container rejected")
	}
	for _, key := range []string{HostUIDLabel, ProjectUIDLabel, ProtocolLabel, "com.docker.compose.project", "com.docker.compose.service"} {
		old := labels[key]
		labels[key] = "other"
		if container.MatchesContainer(container.ContainerID, labels) {
			t.Fatal("identity mismatch accepted", key)
		}
		labels[key] = old
	}
	if container.MatchesContainer(strings.Repeat("b", 64), labels) {
		t.Fatal("replacement container accepted")
	}
	for _, uid := range []string{"", a.HostUID + " ", strings.ToUpper(a.HostUID), "00000000-0000-0000-0000-000000000000"} {
		if uid == a.HostUID {
			continue
		}
		if _, err := NewProjectBinding(uid, a.ProjectUID); err == nil {
			t.Fatal("invalid UID accepted", uid)
		}
	}
}

func TestBindComposePreservesInputAndRejectsAmbiguity(t *testing.T) {
	b, _ := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	for _, labels := range []string{"", "    labels: {purpose: task115}\n", "    labels: [purpose=task115]\n"} {
		input := "name: user-name\nservices:\n  web:\n    image: alpine:3.21\n" + labels
		out, err := BindCompose(input, b)
		if err != nil || !strings.Contains(out, b.Namespace) || !strings.Contains(out, b.ProjectUID) || !strings.Contains(input, "name: user-name") {
			t.Fatal(out, err)
		}
		again, err := BindCompose(out, b)
		if err != nil || again != out {
			t.Fatal("execution binding is not idempotent", err)
		}
	}
	for _, input := range []string{"null", "[]", "services: {}", "services:\n  web:\n    image: one\n    image: two", "services:\n  web:\n    container_name: other", "services:\n  web:\n    labels:\n      io.opensoha.project-uid: other", "services:\n  web:\n    labels: [x=1, x=2]", "services:\n  web:\n    extends: base\n  base:\n    container_name: other", "include: outside.yaml\nservices:\n  web:\n    image: alpine"} {
		if _, err := BindCompose(input, b); err == nil {
			t.Fatal("unsafe Compose accepted", input)
		}
	}
}

func TestRuntimeClaimProtocolsAndStrictContainerDecode(t *testing.T) {
	for _, values := range [][]string{nil, {IdentityProtocol}, {"future-protocol"}} {
		if err := ValidateRuntimeProtocols(values); err != nil {
			t.Fatal(values, err)
		}
	}
	for _, values := range [][]string{{IdentityProtocol, IdentityProtocol}, {""}, {"UPPER"}, {"space value"}, {strings.Repeat("a", 65)}, {"a", "b", "c", "d", "e"}} {
		if ValidateRuntimeProtocols(values) == nil {
			t.Fatal("invalid protocols accepted", values)
		}
	}
	p, _ := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	b := ContainerBinding{ProjectBinding: p, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}
	if decoded, err := ContainerBindingFromValue(b); err != nil || decoded != b {
		t.Fatal(decoded, err)
	}
	if _, err := ContainerBindingFromValue(p); err == nil {
		t.Fatal("project-only target accepted as container")
	}
	if _, err := ProjectBindingFromValue(b); err == nil {
		t.Fatal("extra container fields accepted as project binding")
	}
}
