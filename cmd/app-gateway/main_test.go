package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunCLIArgumentsAndMissingConfig(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, run([]string{"--help"}, func(string) (string, bool) { return "", false }))
	require.Equal(t, 0, run([]string{"-h"}, func(string) (string, bool) { return "", false }))
	require.Equal(t, 2, run([]string{"--unknown"}, func(string) (string, bool) { return "", false }))
	require.Equal(t, 1, run(nil, func(string) (string, bool) { return "", false }))
}
