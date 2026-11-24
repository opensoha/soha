package pluginrelease

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z0-9][a-z0-9._-]*$`)

// RemovePlugin deletes a retired package tree. The caller holds the
// installation lock for this ID and has drained the retired runtime.
func (store *Store) RemovePlugin(ctx context.Context, pluginID string) error {
	if len(pluginID) > 255 || !pluginIDPattern.MatchString(pluginID) {
		return errInventory
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := openPackageRoot(store.root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = root.Close() }()
	child, err := privateChild(root, pluginID, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errPackageRoot
	}
	if err := child.Close(); err != nil {
		return errPackageRoot
	}
	if err := root.RemoveAll(pluginID); err != nil {
		return errPackageRoot
	}
	if err := syncDirectory(root, "."); err != nil {
		return errPackageRoot
	}
	return nil
}
