//go:build linux

package personalguest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func (b *runtimeBroker) file(ctx context.Context, op guest.RuntimeOperation) (*guest.RuntimeFileResult, error) {
	root, err := os.OpenRoot(runtimeWorkspace(b.plan))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return runtimeFile(ctx, root, op)
}

func runtimeFile(ctx context.Context, root *os.Root, op guest.RuntimeOperation) (*guest.RuntimeFileResult, error) {
	if op.File == nil || !guest.RuntimeFilePath(op.File.Path) {
		return nil, errors.New("personal file path refused")
	}
	if op.Kind == "file_read" {
		data, err := readRuntimeFile(root, op.File.Path)
		if err != nil {
			return nil, err
		}
		return &guest.RuntimeFileResult{Path: op.File.Path, ContentBase64: base64.StdEncoding.EncodeToString(data), Size: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
	}
	data, err := base64.StdEncoding.DecodeString(op.File.ContentBase64)
	if err != nil || len(data) > guest.RuntimeFileLimit || !utf8.Valid(data) {
		return nil, errors.New("bounded UTF-8 personal file required")
	}
	old, readErr := readRuntimeFile(root, op.File.Path)
	if readErr == nil && (op.File.ExpectedSHA256 == "" || fmt.Sprintf("%x", sha256.Sum256(old)) != op.File.ExpectedSHA256) || errors.Is(readErr, os.ErrNotExist) && op.File.ExpectedSHA256 != "" {
		return nil, errors.New("personal file revision conflict")
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := root.MkdirAll(filepath.Dir(op.File.Path), 0700); err != nil {
		return nil, err
	}
	name := filepath.Join(filepath.Dir(op.File.Path), ".soha-write-"+uuid.NewString())
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(data); err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := root.Rename(name, op.File.Path); err != nil {
		return nil, err
	}
	dir, err := root.Open(filepath.Dir(op.File.Path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return nil, err
	}
	return &guest.RuntimeFileResult{Path: op.File.Path, Size: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

func readRuntimeFile(root *os.Root, path string) ([]byte, error) {
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Size() > guest.RuntimeFileLimit || stat.Nlink != 1 {
		return nil, errors.New("bounded regular unlinked personal file required")
	}
	data, err := io.ReadAll(io.LimitReader(file, guest.RuntimeFileLimit+1))
	after, statErr := file.Stat()
	if err != nil || statErr != nil || len(data) > guest.RuntimeFileLimit || !utf8.Valid(data) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("personal file changed or exceeded its bound")
	}
	return data, nil
}
