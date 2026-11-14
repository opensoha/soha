//go:build !linux && !darwin

package config

import (
	"errors"
	"os"
)

func openSecretFileNoFollow(string) (*os.File, error) {
	return nil, errors.New("bearer token files are supported only on linux and darwin")
}
