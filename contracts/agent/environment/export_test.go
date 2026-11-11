package environment

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"testing"
)

func TestPersonalDataStreamRejectsIncompleteOrChangedArchive(t *testing.T) {
	fixture, err := os.ReadFile("../../fixtures/json-schema/personal-environment-request/valid/export-data.json")
	if err != nil {
		t.Fatal(err)
	}
	var request Request
	if err := json.Unmarshal(fixture, &request); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("archive fixture\x00"), 5000)
	var stream bytes.Buffer
	writer := ExportWriter{Writer: &stream}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Finish(request); err != nil {
		t.Fatal(err)
	}
	var target bytes.Buffer
	if _, err := ReadExport(bytes.NewReader(stream.Bytes()), &target, request); err != nil || !bytes.Equal(target.Bytes(), data) {
		t.Fatalf("complete binary stream changed: %v", err)
	}
	for _, cut := range []int{0, 4, 500, stream.Len() - 1} {
		if _, err := ReadExport(bytes.NewReader(stream.Bytes()[:cut]), &bytes.Buffer{}, request); err == nil {
			t.Fatal("truncated producer became a completed export")
		}
	}
	changed := bytes.Clone(stream.Bytes())
	changed[5] ^= 1
	if _, err := ReadExport(bytes.NewReader(changed), &bytes.Buffer{}, request); err == nil {
		t.Fatal("corrupt archive bypassed final SHA-256")
	}
	other := request
	other.Lease.OwnerID = "another-owner"
	if _, err := ReadExport(bytes.NewReader(stream.Bytes()), &bytes.Buffer{}, other); err == nil {
		t.Fatal("archive borrowed another owner's receipt")
	}
	if _, err := ReadExport(bytes.NewReader(append(bytes.Clone(stream.Bytes()), 'x')), &bytes.Buffer{}, request); err == nil {
		t.Fatal("trailing stream accepted")
	}
	var invalid bytes.Buffer
	if err := binary.Write(&invalid, binary.BigEndian, uint32(exportChunkBytes+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExport(&invalid, &bytes.Buffer{}, request); err == nil {
		t.Fatal("oversized frame accepted")
	}
	writer.size = ExportMaxBytes
	if _, err := writer.Write([]byte("x")); err == nil {
		t.Fatal("archive bound was silently truncated")
	}
}
