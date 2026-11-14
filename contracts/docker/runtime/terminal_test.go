package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestNativeTerminalInputBindsFullContainerAndSharedPTYLimits(t *testing.T) {
	input := TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "owned"}
	normal, err := input.Normalized()
	if err != nil || normal.Shell != "/bin/sh" || normal.Width != 80 || normal.Height != 24 || normal.TimeoutSeconds != 300 {
		t.Fatalf("defaults=%+v err=%v", normal, err)
	}
	for _, change := range []func(*TerminalInput){
		func(v *TerminalInput) { v.ExpectedContainerID = "prefix" }, func(v *TerminalInput) { v.ServiceName = "../other" },
		func(v *TerminalInput) { v.Shell = "sh -c unsafe" }, func(v *TerminalInput) { v.Width = 1001 }, func(v *TerminalInput) { v.Height = 1001 },
		func(v *TerminalInput) { v.TimeoutSeconds = -1 }, func(v *TerminalInput) { v.TimeoutSeconds = 901 },
	} {
		changed := input
		change(&changed)
		if _, err := changed.Normalized(); err == nil {
			t.Fatal("unsafe terminal input accepted")
		}
	}
}

func TestNativeTerminalRequestRequiresExactBindingAndDeadline(t *testing.T) {
	binding, err := NewProjectBinding("6f773cfe-a522-468c-8c85-4b326d41b781", "65b279e6-ea7c-412f-a822-228349f35585")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input := TerminalRequest{Protocol: TerminalProtocol, ProjectID: "project:owned", Container: ContainerBinding{ProjectBinding: binding, ServiceName: "owned", ContainerID: strings.Repeat("a", 64)}, Input: TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "owned"}, DeadlineAt: now.Add(5 * time.Minute)}
	if _, err := input.Normalized(now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*TerminalRequest){
		func(v *TerminalRequest) { v.Protocol = "old" }, func(v *TerminalRequest) { v.Container.ContainerID = strings.Repeat("b", 64) },
		func(v *TerminalRequest) { v.Container.ServiceName = "foreign" }, func(v *TerminalRequest) { v.DeadlineAt = now },
		func(v *TerminalRequest) { v.DeadlineAt = now.Add(901 * time.Second) }, func(v *TerminalRequest) { v.ProjectID = "\nother" },
		func(v *TerminalRequest) { v.Input.TimeoutSeconds = 1 },
	} {
		changed := input
		change(&changed)
		if _, err := changed.Normalized(now); err == nil {
			t.Fatal("unbound or expired terminal request accepted")
		}
	}
}
