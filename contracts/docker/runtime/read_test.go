package runtime

import (
	"strings"
	"testing"
)

func TestReadRequestRequiresFullIdentityAndActionSpecificBounds(t *testing.T) {
	project, _ := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	base := ReadRequest{Protocol: ReadProtocol, ProjectID: "project", Container: ContainerBinding{ProjectBinding: project, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}}
	for _, action := range []string{"logs", "volumes", "files.list", "files.read"} {
		t.Run(action, func(t *testing.T) {
			r := base
			r.Action = action
			if strings.HasPrefix(action, "files.") {
				r.Target, r.Path = "/data", "."
			}
			if _, err := r.Normalized(action); err != nil {
				t.Fatal(err)
			}
			r.Container.ContainerID = "short"
			if _, err := r.Normalized(action); err == nil {
				t.Fatal("short CID accepted")
			}
		})
	}
	for name, change := range map[string]func(*ReadRequest){
		"protocol":         func(r *ReadRequest) { r.Protocol = "old" },
		"action":           func(r *ReadRequest) { r.Action = "volumes" },
		"project control":  func(r *ReadRequest) { r.ProjectID = "a\nb" },
		"negative tail":    func(r *ReadRequest) { r.TailLines = -1 },
		"large tail":       func(r *ReadRequest) { r.TailLines = 2001 },
		"unbounded bytes":  func(r *ReadRequest) { r.LimitBytes = 1<<20 + 1 },
		"unexpected mount": func(r *ReadRequest) { r.Target = "/data" },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			r.Action = "logs"
			change(&r)
			if _, err := r.Normalized("logs"); err == nil {
				t.Fatal("invalid log request accepted")
			}
		})
	}
}

func TestFilePathsNeverNormalizeTraversalOrControlCharacters(t *testing.T) {
	for _, value := range []string{"", ".", "a/b", "文件名.txt", "a b"} {
		if !ValidRelativePath(value) {
			t.Fatal("valid path rejected", value)
		}
	}
	for _, value := range []string{"/etc/passwd", "../outside", "a/../../outside", "a/../outside", "a//b", `a\b`, "a\nfile", "\x00", string([]byte{0xff}), strings.Repeat("a", 4097)} {
		if ValidRelativePath(value) {
			t.Fatal("unsafe path accepted", value)
		}
	}
	for _, value := range []string{"/", "/data/../etc", "/data/", `C:\data`, "/data\x00"} {
		if ValidMountTarget(value) {
			t.Fatal("unsafe mount target accepted", value)
		}
	}
}
