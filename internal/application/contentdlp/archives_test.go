package contentdlp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/contentdlp"
)

type archiveTestFile struct {
	name    string
	data    []byte
	mode    os.FileMode
	store   bool
	comment string
}

func testZIP(t testing.TB, entries ...archiveTestFile) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, item := range entries {
		h := &zip.FileHeader{Name: item.name, Method: zip.Deflate, Comment: item.comment}
		if item.store {
			h.Method = zip.Store
		}
		if item.mode != 0 {
			h.SetMode(item.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testGZIP(t testing.TB, data []byte) []byte {
	return testGZIPHeader(t, data, gzip.Header{})
}

func testGZIPHeader(t testing.TB, data []byte, header gzip.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	w.Header = header
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testTAR(t testing.TB, name string, data []byte, kind byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	h := &tar.Header{Name: name, Mode: 0600, Typeflag: kind, Format: tar.FormatUSTAR, Size: int64(len(data))}
	if kind == tar.TypeSymlink {
		h.Linkname = "other"
		h.Size = 0
	}
	if err := w.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if kind == tar.TypeReg {
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

type archiveBoundaryCase struct {
	name   string
	data   []byte
	status string
}

func archiveBoundaryCases(t testing.TB) []archiveBoundaryCase {
	secret := []byte("password = synthetic_archive_value")
	plain := testZIP(t, archiveTestFile{name: "safe.txt", data: []byte("ordinary complete text")})
	matched := testZIP(t, archiveTestFile{name: "key.txt", data: secret})
	nested := testZIP(t, archiveTestFile{name: "nested.tar.gz", data: testGZIP(t, testTAR(t, "key.txt", secret, tar.TypeReg))})
	deep := nested
	for i := 0; i < 2; i++ {
		deep = testZIP(t, archiveTestFile{name: "nested.zip", data: deep})
	}
	corrupt := bytes.Clone(plain)
	// Corrupt both declarations so framing remains consistent while the actual
	// expanded bytes no longer match the declared checksum.
	central := bytes.Index(corrupt, []byte("PK\x01\x02"))
	if central < 16 || string(corrupt[central-16:central-12]) != "PK\x07\x08" {
		t.Fatal("fixture lacks a complete checksum descriptor")
	}
	corrupt[central+16] ^= 1
	corrupt[central-12] ^= 1
	encrypted := bytes.Clone(matched)
	for _, signature := range []string{"PK\x03\x04", "PK\x01\x02"} {
		position := bytes.Index(encrypted, []byte(signature))
		flag := 6
		if signature == "PK\x01\x02" {
			flag = 8
		}
		binary.LittleEndian.PutUint16(encrypted[position+flag:], binary.LittleEndian.Uint16(encrypted[position+flag:])|1)
	}
	many := []archiveTestFile{}
	for i := 0; i < 65; i++ {
		many = append(many, archiveTestFile{name: fmt.Sprintf("%d.txt", i), data: []byte("safe")})
	}
	padding := []byte{}
	for i := 0; i < 200; i++ {
		padding = append(padding, fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(i))))...)
	}
	localName := bytes.Clone(plain)
	localName[30] = 'X'
	badDirectory := testZIP(t, archiveTestFile{name: "folder/", store: true})
	directoryCentral := bytes.Index(badDirectory, []byte("PK\x01\x02"))
	badDirectory[directoryCentral+16] = 1
	badDirectory[14] = 1
	largeText := []byte(strings.Repeat(string(padding), 12))
	tarTrailing := append(testTAR(t, "test.txt", []byte("safe"), tar.TypeReg), make([]byte, 1536)...)
	tarTrailing[len(tarTrailing)-1536] = 1
	cases := []archiveBoundaryCase{
		{"zip clean", plain, "clean"}, {"zip matched", matched, "matched"}, {"three layers", nested, "matched"},
		{"stored zip", testZIP(t, archiveTestFile{name: "test.txt", data: secret, store: true}), "matched"},
		{"empty zip", testZIP(t), "clean"},
		{"directory", testZIP(t, archiveTestFile{name: "folder/", store: true}), "clean"},
		{"directory CRC", badDirectory, "error"},
		{"gzip matched", testGZIP(t, secret), "matched"}, {"tar matched", testTAR(t, "key.txt", secret, tar.TypeReg), "matched"},
		{"crc failure", corrupt, "error"}, {"encrypted", encrypted, "encrypted"}, {"truncated", plain[:len(plain)-5], "error"},
		{"trailing", append(bytes.Clone(plain), []byte("hidden")...), "error"},
		{"gzip concatenation", append(testGZIP(t, []byte("safe")), testGZIP(t, secret)...), "error"},
		{"unsafe path", testZIP(t, archiveTestFile{name: "../hidden.txt", data: secret}), "unsupported"},
		{"duplicate path", testZIP(t, archiveTestFile{name: "same.txt"}, archiveTestFile{name: "SAME.txt", data: secret}), "unsupported"},
		{"zip link", testZIP(t, archiveTestFile{name: "link", data: []byte("target"), mode: os.ModeSymlink | 0777}), "unsupported"},
		{"tar link", testTAR(t, "link", nil, tar.TypeSymlink), "unsupported"},
		{"unsupported after match", testZIP(t, archiveTestFile{name: "key.txt", data: secret}, archiveTestFile{name: "picture.png", data: []byte{0, 1, 2}}), "unsupported"},
		{"office container", testZIP(t, archiveTestFile{name: "[Content_Types].xml", data: []byte("<Types/>")}), "unsupported"},
		{"pdf member", testZIP(t, archiveTestFile{name: "document.pdf", data: []byte("%PDF-1.7")}), "unsupported"},
		{"PGP member", testZIP(t, archiveTestFile{name: "secret.asc", data: []byte("-----BEGIN PGP MESSAGE-----")}), "encrypted"},
		{"entry count", testZIP(t, many...), "too_large"}, {"depth", deep, "too_large"},
		{"ratio", testZIP(t, archiveTestFile{name: "bomb.txt", data: []byte(strings.Repeat("a", 64000))}), "too_large"},
		{"member ratio despite padding", testZIP(t, archiveTestFile{name: "bomb.txt", data: []byte(strings.Repeat("a", 64000))}, archiveTestFile{name: "padding.txt", data: padding}), "too_large"},
		{"local name mismatch", localName, "error"},
		{"cumulative expansion", testZIP(t, archiveTestFile{name: "first.txt", data: largeText}, archiveTestFile{name: "second.txt", data: largeText}), "too_large"},
		{"raw input limit", make([]byte, domain.MaxTextBytes+1), "too_large"},
		{"tar trailing payload", tarTrailing, "error"},
		{"name match", testZIP(t, archiveTestFile{name: "password=synthetic_archive_value"}), "matched"},
		{"zip member comment", testZIP(t, archiveTestFile{name: "safe.txt", data: []byte("ordinary"), comment: string(secret)}), "matched"},
		{"gzip name", testGZIPHeader(t, []byte("ordinary"), gzip.Header{Name: "password=synthetic_archive_value"}), "matched"},
		{"gzip comment", testGZIPHeader(t, []byte("ordinary"), gzip.Header{Comment: string(secret)}), "matched"},
		{"gzip opaque extra", testGZIPHeader(t, []byte("ordinary"), gzip.Header{Extra: []byte("hidden")}), "unsupported"},
	}
	return cases
}

func TestArchiveInspectionBoundaries(t *testing.T) {
	for _, tc := range archiveBoundaryCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"block", "warn", "audit"} {
				c := domain.Content{Enabled: true, Mode: mode, DetectorVersion: domain.ArchiveDetectorVersion, Detectors: []string{"credential_assignment"}, Rules: []domain.Rule{}}
				got := Inspect(context.Background(), c, tc.data)
				if got.Status != tc.status || got.Allowed != (mode == "audit" || tc.status == "clean") || got.DetectorVersion != domain.ArchiveDetectorVersion {
					t.Fatalf("mode=%s decision=%+v want=%s", mode, got, tc.status)
				}
				if tc.status != "clean" && tc.status != "matched" && (got.DataLevel != "unknown" || got.StepUpRequired) {
					t.Fatalf("unknown released: %+v", got)
				}
			}
		})
	}
}

func TestArchiveVersionAndCancellation(t *testing.T) {
	matched := testZIP(t, archiveTestFile{name: "key.txt", data: []byte("password=synthetic_archive_value")})
	c := domain.Content{Mode: "block", Detectors: []string{"credential_assignment"}, Rules: []domain.Rule{}}
	if got := Inspect(context.Background(), c, matched); got.Status != "unsupported" || got.DetectorVersion != domain.DetectorVersion {
		t.Fatalf("legacy changed: %+v", got)
	}
	legacyHash := RuleSetVersion(c)
	c.DetectorVersion = domain.DetectorVersion
	if RuleSetVersion(c) != legacyHash {
		t.Fatal("explicit old pin changed legacy rule digest")
	}
	c.DetectorVersion = domain.ArchiveDetectorVersion
	if RuleSetVersion(c) == legacyHash {
		t.Fatal("archive pin absent from rule digest")
	}
	if ValidatePolicy(c) != nil || validateDockerPolicy(c) == nil {
		t.Fatal("archive support crossed the Docker string boundary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Inspect(ctx, c, matched); got.Status != "timeout" || got.Allowed {
		t.Fatalf("cancel ignored: %+v", got)
	}
}

func TestZIPUntrustedSizeCannotOverflow(t *testing.T) {
	data := testZIP(t, archiveTestFile{name: "test.txt", data: []byte("ordinary")})
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f := *r.File[0]
	f.CompressedSize64 = ^uint64(0)
	central := bytes.Index(data, []byte("PK\x01\x02"))
	if _, status := zipLocalRecord(data, central, 0, &f); status != "error" {
		t.Fatal("untrusted ZIP64 length overflowed")
	}
}

func FuzzArchiveInspectionNeverPanics(f *testing.F) {
	f.Add(testZIP(f, archiveTestFile{name: "test.txt", data: []byte("password=synthetic_archive_value")}))
	f.Add(testTAR(f, "test.txt", []byte("safe"), tar.TypeReg))
	f.Add(testGZIP(f, []byte("password=synthetic_archive_value")))
	for _, seed := range [][]byte{[]byte("PK\x03\x04"), []byte("PK\x05\x06"), {0x1f, 0x8b}, []byte("ordinary")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > domain.MaxTextBytes {
			t.Skip()
		}
		c := domain.Content{Mode: "block", DetectorVersion: domain.ArchiveDetectorVersion, Detectors: []string{"private_key"}, Rules: []domain.Rule{}}
		got := Inspect(context.Background(), c, data)
		if got.Status != "clean" && got.Status != "matched" && got.Allowed {
			t.Fatalf("uninspected allowed: %+v", got)
		}
	})
}
