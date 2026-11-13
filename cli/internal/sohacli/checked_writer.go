package sohacli

import (
	"fmt"
	"io"
)

type checkedWriter struct {
	out io.Writer
	err error
}

func newCheckedWriter(out io.Writer) *checkedWriter {
	return &checkedWriter{out: out}
}

func (w *checkedWriter) Print(values ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprint(w.out, values...)
	}
}

func (w *checkedWriter) Printf(format string, values ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprintf(w.out, format, values...)
	}
}

func (w *checkedWriter) Println(values ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprintln(w.out, values...)
	}
}

func (w *checkedWriter) Err() error {
	return w.err
}
