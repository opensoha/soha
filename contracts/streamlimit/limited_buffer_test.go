package streamlimit

import (
	"strings"
	"testing"
)

func TestLimitedBufferTruncatesAtLimit(t *testing.T) {
	buffer := NewLimitedBuffer(5)
	if _, err := buffer.Write([]byte("hello world")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := buffer.String(); got != "hello" {
		t.Fatalf("String() = %q, want hello", got)
	}
	if got := buffer.TotalBytes(); got != 11 {
		t.Fatalf("TotalBytes() = %d, want 11", got)
	}
	if !buffer.Truncated() {
		t.Fatalf("Truncated() = false, want true")
	}
}

func TestReadStringReturnsFullContentWhenWithinLimit(t *testing.T) {
	content, totalBytes, truncated, err := ReadString(strings.NewReader("hello"), 16)
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	if content != "hello" {
		t.Fatalf("content = %q, want hello", content)
	}
	if totalBytes != 5 {
		t.Fatalf("totalBytes = %d, want 5", totalBytes)
	}
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
}
