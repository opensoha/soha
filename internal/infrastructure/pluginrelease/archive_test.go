//go:build unix

package pluginrelease

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

func TestWebArchiveRejectsAmbiguousAndUnsafeEntries(t *testing.T) {
	t.Parallel()
	valid := webTarEntry{name: "index.html", data: "<html></html>"}
	tests := []struct {
		name    string
		entries []webTarEntry
		ok      bool
	}{
		{"root and relative paths", []webTarEntry{{name: "./", kind: tar.TypeDir}, {name: "./index.html", data: "hello"}, {name: "a/main.js", data: "script"}, {name: "./a/", kind: tar.TypeDir}}, true},
		{"missing root index", []webTarEntry{{name: "elsewhere/index.html", data: "hello"}}, false},
		{"directory at root index", []webTarEntry{{name: "index.html", kind: tar.TypeDir}}, false},
		{"backslash", []webTarEntry{valid, {name: "a\\script.js"}}, false},
		{"absolute path", []webTarEntry{valid, {name: "/script.js"}}, false},
		{"dot dot", []webTarEntry{valid, {name: "a/../script.js"}}, false},
		{"dot segment", []webTarEntry{valid, {name: "a/./script.js"}}, false},
		{"empty segment", []webTarEntry{valid, {name: "a//script.js"}}, false},
		{"symlink", []webTarEntry{valid, {name: "a", kind: tar.TypeSymlink}}, false},
		{"hard link", []webTarEntry{valid, {name: "a", kind: tar.TypeLink}}, false},
		{"FIFO", []webTarEntry{valid, {name: "a", kind: tar.TypeFifo}}, false},
		{"duplicate canonical path", []webTarEntry{valid, {name: "./index.html", data: "another"}}, false},
		{"case alias", []webTarEntry{valid, {name: "Index.HTML", data: "another"}}, false},
		{"implicit directory conflict", []webTarEntry{valid, {name: "a/script.js"}, {name: "a"}}, false},
		{"parent conflict", []webTarEntry{valid, {name: "a"}, {name: "a/script.js"}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			archive := siteArchive(t, test.entries)
			_, err := readWebArchive(t.Context(), bytes.NewReader(archive), consumeWebPath)
			if (test.ok && err != nil) || (!test.ok && !errors.Is(err, errArtifact)) {
				t.Fatalf("readWebArchive() = %v", err)
			}
		})
	}
}

func TestWebArchiveVerifiesGzipTrailer(t *testing.T) {
	t.Parallel()
	archive := siteArchive(t, []webTarEntry{{name: "index.html", data: "valid"}})
	combined := append(append([]byte(nil), archive...), archive...)
	badChecksum := append([]byte(nil), archive...)
	badChecksum[len(badChecksum)-8] ^= 0xff
	for name, data := range map[string][]byte{"multiple members": combined, "invalid CRC": badChecksum} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := readWebArchive(t.Context(), bytes.NewReader(data), consumeWebPath); !errors.Is(err, errArtifact) {
				t.Fatalf("readWebArchive() = %v", err)
			}
		})
	}
}

func TestWebArchiveBoundsHeaderCountIncludingIgnoredRootEntries(t *testing.T) {
	t.Parallel()
	entries := slices.Repeat([]webTarEntry{{name: "./", kind: tar.TypeDir}}, maxWebPaths)
	entries = append(entries, webTarEntry{name: "index.html"})
	archive := siteArchive(t, entries)
	if _, err := readWebArchive(t.Context(), bytes.NewReader(archive), consumeWebPath); !errors.Is(err, errArtifact) {
		t.Fatalf("readWebArchive() = %v", err)
	}
}

func consumeWebPath(entry webPath, input io.Reader) error {
	if entry.dir {
		return nil
	}
	_, err := io.Copy(io.Discard, input)
	return err
}
