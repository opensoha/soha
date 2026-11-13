package sohacli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func readHiddenLine(rt Runtime, trimSpace bool) (string, error) {
	if file, ok := rt.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		raw, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := fmt.Fprintln(rt.Err); writeErr != nil && err == nil {
			err = writeErr
		}
		if err != nil {
			return "", err
		}
		if trimSpace {
			return strings.TrimSpace(string(raw)), nil
		}
		return string(raw), nil
	}
	line, err := bufio.NewReader(rt.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if trimSpace {
		return strings.TrimSpace(line), nil
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
