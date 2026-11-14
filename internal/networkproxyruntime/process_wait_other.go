//go:build !linux && !darwin

package networkproxyruntime

import "errors"

func waitOwnedProcessExit(_ int) error {
	return errors.New("non-reaping process wait unavailable")
}
