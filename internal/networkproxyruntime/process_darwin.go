//go:build darwin

package networkproxyruntime

import "golang.org/x/sys/unix"

func waitOwnedProcessExit(pid int) error {
	queue, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(queue)
	var event unix.Kevent_t
	unix.SetKevent(&event, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	event.Fflags = unix.NOTE_EXIT
	// Register separately so an interrupted blocking wait cannot consume and
	// lose the one-shot exit event while re-registering it.
	if _, err = unix.Kevent(queue, []unix.Kevent_t{event}, nil, nil); err != nil {
		if err == unix.ESRCH {
			// The started child can exit before registration. The caller still
			// owns its unreaped PID and will release it with Cmd.Wait.
			return nil
		}
		return err
	}
	for {
		var result [1]unix.Kevent_t
		if _, err = unix.Kevent(queue, nil, result[:], nil); err != unix.EINTR {
			return err
		}
	}
}
