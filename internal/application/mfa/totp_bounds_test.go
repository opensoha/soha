package mfa

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTOTPRejectsNegativeCounter(t *testing.T) {
	code, err := totpCode("", -1)
	require.Error(t, err)
	require.Empty(t, code)
}
