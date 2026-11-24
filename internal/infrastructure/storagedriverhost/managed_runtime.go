package storagedriverhost

import "os"

// ManagedRuntime holds an exclusive lease on a parent of per-supervisor
// runtime directories. The owner must keep the lease until every child has
// drained and its directory has been cleaned up.
type ManagedRuntime struct {
	root string
	lock *os.File
}

func (r *ManagedRuntime) Close() error { return r.lock.Close() }
