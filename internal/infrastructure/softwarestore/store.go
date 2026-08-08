package softwarestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	appsoftware "github.com/opensoha/soha/internal/application/software"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Store struct {
	root  string
	blobs string
	mu    sync.Mutex
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("software storage directory is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve software storage directory: %w", err)
	}
	blobs := filepath.Join(root, "blobs")
	if err := os.MkdirAll(blobs, 0o750); err != nil {
		return nil, fmt.Errorf("create software storage directory: %w", err)
	}
	return &Store{root: root, blobs: blobs}, nil
}

func (s *Store) List(_ context.Context, filter appsoftware.Filter) ([]appsoftware.Package, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	items, err := s.load()
	if err != nil {
		return nil, "", err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	filtered := make([]appsoftware.Package, 0, len(items))
	for _, item := range items {
		if filter.Platform != "" && item.Platform != filter.Platform || filter.Arch != "" && item.Arch != filter.Arch {
			continue
		}
		filtered = append(filtered, item)
	}
	return paginate(filtered, filter.Cursor, filter.Limit)
}

func (s *Store) Storage(_ context.Context, cursor string, limit int) (appsoftware.Storage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.load()
	if err != nil {
		return appsoftware.Storage{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	var totalBytes int64
	for _, item := range items {
		totalBytes += item.SizeBytes
	}
	page, next, err := paginate(items, cursor, limit)
	if err != nil {
		return appsoftware.Storage{}, err
	}
	return appsoftware.Storage{
		Backend: "filesystem", ObjectCount: int64(len(items)), TotalBytes: totalBytes,
		Items: page, NextCursor: next,
	}, nil
}

func paginate(items []appsoftware.Package, cursor string, limit int) ([]appsoftware.Package, string, error) {
	start := 0
	if cursor != "" {
		start = -1
		for i := range items {
			if items[i].ID == cursor {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return nil, "", fmt.Errorf("%w: invalid software package cursor", apperrors.ErrInvalidArgument)
		}
	}
	end := min(start+limit, len(items))
	next := ""
	if end < len(items) && end > start {
		next = items[end-1].ID
	}
	return append([]appsoftware.Package(nil), items[start:end]...), next, nil
}

func (s *Store) Create(_ context.Context, input appsoftware.UploadInput, content io.Reader) (appsoftware.Package, error) {
	temp, err := os.CreateTemp(s.blobs, ".upload-*")
	if err != nil {
		return appsoftware.Package{}, fmt.Errorf("create software package temporary file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return appsoftware.Package{}, fmt.Errorf("secure software package temporary file: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(content, appsoftware.MaxPackageBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return appsoftware.Package{}, fmt.Errorf("store software package: %w", copyErr)
	}
	if closeErr != nil {
		return appsoftware.Package{}, fmt.Errorf("close software package: %w", closeErr)
	}
	if size < 1 || size > appsoftware.MaxPackageBytes {
		return appsoftware.Package{}, fmt.Errorf("%w: installer must be between 1 and %d bytes", apperrors.ErrInvalidArgument, appsoftware.MaxPackageBytes)
	}
	id, err := randomID()
	if err != nil {
		return appsoftware.Package{}, err
	}
	now := time.Now().UTC()
	item := appsoftware.Package{
		ID: id, SoftwareID: input.SoftwareID, Name: input.Name, Description: input.Description,
		Publisher: input.Publisher, Category: input.Category, Version: input.Version,
		Platform: input.Platform, Arch: input.Arch, FileName: input.FileName,
		SizeBytes: size, SHA256: hex.EncodeToString(hash.Sum(nil)),
		DownloadPath: "/api/v1/software/packages/" + id + "/download", CreatedAt: now, UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.load()
	if err != nil {
		return appsoftware.Package{}, err
	}
	for _, existing := range items {
		if existing.SoftwareID == item.SoftwareID && existing.Version == item.Version && existing.Platform == item.Platform && existing.Arch == item.Arch {
			return appsoftware.Package{}, fmt.Errorf("%w: software package version already exists for platform and architecture", apperrors.ErrConflict)
		}
	}
	finalName := s.blobPath(id)
	if err := os.Rename(tempName, finalName); err != nil {
		return appsoftware.Package{}, fmt.Errorf("commit software package file: %w", err)
	}
	if err := s.save(append(items, item)); err != nil {
		_ = os.Remove(finalName)
		return appsoftware.Package{}, err
	}
	return item, nil
}

func (s *Store) Open(_ context.Context, id string) (appsoftware.Package, io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.load()
	if err != nil {
		return appsoftware.Package{}, nil, err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		file, err := os.Open(s.blobPath(id))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return appsoftware.Package{}, nil, fmt.Errorf("%w: software package %s", apperrors.ErrNotFound, id)
			}
			return appsoftware.Package{}, nil, fmt.Errorf("open software package: %w", err)
		}
		return item, file, nil
	}
	return appsoftware.Package{}, nil, fmt.Errorf("%w: software package %s", apperrors.ErrNotFound, id)
}

func (s *Store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.load()
	if err != nil {
		return err
	}
	index := -1
	for i := range items {
		if items[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("%w: software package %s", apperrors.ErrNotFound, id)
	}
	if err := s.save(append(items[:index:index], items[index+1:]...)); err != nil {
		return err
	}
	if err := os.Remove(s.blobPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete software package file: %w", err)
	}
	return nil
}

func (s *Store) load() ([]appsoftware.Package, error) {
	content, err := os.ReadFile(filepath.Join(s.root, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []appsoftware.Package{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read software package index: %w", err)
	}
	var items []appsoftware.Package
	if err := json.Unmarshal(content, &items); err != nil {
		return nil, fmt.Errorf("decode software package index: %w", err)
	}
	return items, nil
}

func (s *Store) save(items []appsoftware.Package) error {
	temp, err := os.CreateTemp(s.root, ".index-*")
	if err != nil {
		return fmt.Errorf("create software package index: %w", err)
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure software package index: %w", err)
	}
	if err := json.NewEncoder(temp).Encode(items); err != nil {
		_ = temp.Close()
		return fmt.Errorf("encode software package index: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close software package index: %w", err)
	}
	if err := os.Rename(name, filepath.Join(s.root, "index.json")); err != nil {
		return fmt.Errorf("commit software package index: %w", err)
	}
	return nil
}

func (s *Store) blobPath(id string) string { return filepath.Join(s.blobs, id+".blob") }

func randomID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate software package id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
