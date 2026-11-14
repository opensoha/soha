package sohacli

import (
	"errors"
	"testing"
)

type failingWriter struct {
	err   error
	calls int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

func TestCheckedWriterKeepsFirstError(t *testing.T) {
	want := errors.New("write failed")
	destination := &failingWriter{err: want}
	out := newCheckedWriter(destination)

	out.Printf("first: %s", "value")
	out.Println("second")

	if !errors.Is(out.Err(), want) {
		t.Fatalf("expected %v, got %v", want, out.Err())
	}
	if destination.calls != 1 {
		t.Fatalf("expected one write attempt, got %d", destination.calls)
	}
}
