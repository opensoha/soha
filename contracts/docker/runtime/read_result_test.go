package runtime

import (
	"strings"
	"testing"
)

func readFixture(action string) (ReadRequest, ReadResult) {
	project, _ := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	req := ReadRequest{Protocol: ReadProtocol, Action: action, ProjectID: "project", Container: ContainerBinding{ProjectBinding: project, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}}
	if strings.HasPrefix(action, "files.") {
		req.Target = "/data"
	}
	req, _ = req.Normalized(action)
	r := ReadResult{Protocol: ReadProtocol, Action: action, ProjectID: req.ProjectID, Container: req.Container, Target: req.Target, Complete: true, Volumes: []Volume{}, Files: []FileEntry{}}
	if action == "logs" || action == "files.read" {
		r.Encoding = "utf-8"
	}
	return req, r
}

func TestReadResultBindsExactRequestAndExplicitCompleteness(t *testing.T) {
	for _, action := range []string{"logs", "volumes", "files.list", "files.read"} {
		t.Run(action, func(t *testing.T) {
			req, result := readFixture(action)
			if err := result.ValidateFor(req); err != nil {
				t.Fatal(err)
			}
			for name, change := range map[string]func(*ReadResult){
				"replacement":                func(r *ReadResult) { r.Container.ContainerID = strings.Repeat("b", 64) },
				"project":                    func(r *ReadResult) { r.ProjectID = "other" },
				"protocol":                   func(r *ReadResult) { r.Protocol = "old" },
				"target":                     func(r *ReadResult) { r.Target = "/other" },
				"missing inventory":          func(r *ReadResult) { r.Volumes = nil },
				"missing files":              func(r *ReadResult) { r.Files = nil },
				"unknown completeness":       func(r *ReadResult) { r.Complete = false },
				"contradictory completeness": func(r *ReadResult) { r.Truncated = true },
			} {
				t.Run(name, func(t *testing.T) {
					r := result
					change(&r)
					if r.ValidateFor(req) == nil {
						t.Fatal("invalid result accepted")
					}
				})
			}
		})
	}
}

func TestReadResultRejectsEscapesSpecialFilesAndUnboundedText(t *testing.T) {
	req, r := readFixture("files.list")
	r.Files = []FileEntry{{Name: "文件.txt", Path: "文件.txt", Kind: "file", ReadSupported: true}}
	if err := r.ValidateFor(req); err != nil {
		t.Fatal(err)
	}
	for _, file := range []FileEntry{
		{Name: "../outside", Path: "../outside", Kind: "file"},
		{Name: "file", Path: "elsewhere/file", Kind: "file"},
		{Name: "link", Path: "link", Kind: "symlink", ReadSupported: true},
		{Name: "fifo", Path: "fifo", Kind: "other", ListSupported: true},
		{Name: "dir", Path: "dir", Kind: "directory", ReadSupported: true},
	} {
		r.Files = []FileEntry{file}
		if r.ValidateFor(req) == nil {
			t.Fatal("unsafe entry accepted", file)
		}
	}
	req, r = readFixture("logs")
	for _, content := range []string{string([]byte{0xff}), "binary\x00", strings.Repeat("x", int(req.LimitBytes)+1)} {
		r.Content, r.SizeBytes = content, int64(len(content))
		if r.ValidateFor(req) == nil {
			t.Fatal("unsafe or oversized text accepted")
		}
	}
	req, r = readFixture("volumes")
	r.Volumes = []Volume{{Target: "/data", Type: "bind", UnsupportedReason: "reader disabled"}}
	if err := r.ValidateFor(req); err != nil {
		t.Fatal(err)
	}
	r.Volumes = append(r.Volumes, r.Volumes[0])
	if r.ValidateFor(req) == nil {
		t.Fatal("duplicate mount accepted")
	}
}

func TestFileReadReportsActualSourceSizeSeparatelyFromReturnedText(t *testing.T) {
	req, r := readFixture("files.read")
	r.Content, r.ReturnedBytes, r.SizeBytes = "中", 3, 20
	r.Truncated, r.Complete = true, false
	if err := r.ValidateFor(req); err != nil {
		t.Fatal(err)
	}
	r.ReturnedBytes = 2
	if r.ValidateFor(req) == nil {
		t.Fatal("incorrect UTF-8 payload size accepted")
	}
	r.ReturnedBytes = 3
	r.SizeBytes = 3
	if r.ValidateFor(req) == nil {
		t.Fatal("truncation without additional source bytes accepted")
	}
	r.Truncated, r.Complete = false, true
	if err := r.ValidateFor(req); err != nil {
		t.Fatal(err)
	}
	r.SizeBytes = 20
	if r.ValidateFor(req) == nil {
		t.Fatal("incomplete file claimed complete")
	}
}
