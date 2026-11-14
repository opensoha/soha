package runner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
	"github.com/opensoha/soha/agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type outpostConfigFloor struct {
	Managed   bool                 `json:"managed,omitempty"`
	Control   *outpostControlFloor `json:"control,omitempty"`
	OutpostID string               `json:"outpostId"`
	Version   int64                `json:"version"`
	Digest    string               `json:"digest"`
	IssuedAt  time.Time            `json:"issuedAt"`
	ExpiresAt time.Time            `json:"expiresAt"`
}

func (f outpostConfigFloor) sameLease(next outpostConfigFloor) bool {
	return f.OutpostID == next.OutpostID && f.Version == next.Version && f.Digest == next.Digest && f.IssuedAt.Equal(next.IssuedAt) && f.ExpiresAt.Equal(next.ExpiresAt)
}

// Keep each replica's lock separate, while importing the pre-instance replay floor.
func openOutpostInstanceState(dir, baseURL, agentID, instanceID string) (*outpostStateStore, outpostConfigFloor, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(instanceID) == "" {
		return nil, outpostConfigFloor{}, errors.New("outpost state requires agent and instance identity")
	}
	store, floor, err := openOutpostState(dir, baseURL, agentID+"\x00"+instanceID)
	if err != nil {
		return nil, outpostConfigFloor{}, err
	}
	if floor.Version != 0 || floor.Managed {
		return store, floor, nil
	}
	legacy, previous, err := openOutpostStateWithLock(dir, baseURL, agentID, true)
	if err != nil {
		store.close()
		return nil, outpostConfigFloor{}, err
	}
	defer legacy.close()
	if previous.Version != 0 {
		if err := store.save(previous); err != nil {
			store.close()
			return nil, outpostConfigFloor{}, err
		}
		floor = previous
	}
	return store, floor, nil
}

func newOutpostConfigFloor(c sohaapi.IdentityOutpostRuntimeConfig) outpostConfigFloor {
	floor := outpostConfigFloor{OutpostID: c.OutpostID, Version: c.ConfigurationVersion, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt}
	// Lease dates and the signing key may change during a same-version renewal.
	// All routing authority remains bound to the version's content digest.
	c.IssuedAt, c.ExpiresAt, c.KeyID, c.Signature = time.Time{}, time.Time{}, "", ""
	digest := sha256.Sum256(outpostConfigPayload(c))
	floor.Digest = hex.EncodeToString(digest[:])
	return floor
}

func (f outpostConfigFloor) accepts(next outpostConfigFloor) error {
	if f.Version == 0 {
		return nil
	}
	if next.OutpostID != f.OutpostID || next.Version < f.Version || next.IssuedAt.Before(f.IssuedAt) {
		return errors.New("outpost configuration regresses durable floor")
	}
	if next.Version == f.Version && (next.Digest != f.Digest || !next.IssuedAt.After(f.IssuedAt) || !next.ExpiresAt.After(f.ExpiresAt)) {
		return errors.New("outpost configuration content changed or lease is not newer")
	}
	return nil
}

type outpostStateStore struct {
	root *os.Root
	name string
	lock *flock.Flock
}

func openOutpostState(dir, baseURL, agentID string) (*outpostStateStore, outpostConfigFloor, error) {
	return openOutpostStateWithLock(dir, baseURL, agentID, false)
}

// Legacy imports only read the old floor. Readers may overlap, but must
// still exclude a legacy runtime that owns the old state for writes.
func openOutpostStateWithLock(dir, baseURL, agentID string, shared bool) (*outpostStateStore, outpostConfigFloor, error) {
	if strings.TrimSpace(dir) == "" {
		dir = ".soha/outpost"
	}
	if strings.TrimSpace(agentID) == "" {
		return nil, outpostConfigFloor{}, errors.New("outpost state requires agent identity")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, outpostConfigFloor{}, err
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, outpostConfigFloor{}, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, outpostConfigFloor{}, err
	}
	identity := sha256.Sum256([]byte(strings.TrimRight(baseURL, "/") + "\x00" + agentID))
	name := hex.EncodeToString(identity[:]) + ".json"
	store := &outpostStateStore{root: root, name: name, lock: flock.New(filepath.Join(abs, name+".lock"))}
	floor, err := store.load(abs, shared)
	if err != nil {
		store.close()
		return nil, outpostConfigFloor{}, err
	}
	return store, floor, nil
}

func (s *outpostStateStore) load(dir string, shared bool) (outpostConfigFloor, error) {
	file, err := s.root.OpenFile(s.name+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_ = file.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return outpostConfigFloor{}, err
	}
	if _, err := config.ReadPrivateFile(filepath.Join(dir, s.name+".lock"), 64); err != nil {
		return outpostConfigFloor{}, err
	}
	tryLock := s.lock.TryLock
	if shared {
		tryLock = s.lock.TryRLock
	}
	locked, err := tryLock()
	if err != nil || !locked {
		return outpostConfigFloor{}, errors.New("another outpost runtime owns this durable state")
	}
	raw, err := config.ReadPrivateFile(filepath.Join(dir, s.name), 16384)
	if errors.Is(err, os.ErrNotExist) {
		return outpostConfigFloor{}, nil
	}
	if err != nil {
		return outpostConfigFloor{}, err
	}
	var floor outpostConfigFloor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&floor) != nil || decoder.Decode(new(any)) != io.EOF || !validOutpostFloor(floor) {
		return outpostConfigFloor{}, errors.New("invalid outpost durable state")
	}
	return floor, nil
}

func validOutpostFloor(f outpostConfigFloor) bool {
	if !validOutpostControlFloor(f.Managed, f.Control) {
		return false
	}
	if f.Version == 0 {
		return f.Managed && f.Control == nil && f.OutpostID == "" && f.Digest == "" && f.IssuedAt.IsZero() && f.ExpiresAt.IsZero()
	}
	if f.Version < 0 || f.OutpostID == "" || len(f.Digest) != 64 || f.IssuedAt.IsZero() || !f.ExpiresAt.After(f.IssuedAt) {
		return false
	}
	_, err := hex.DecodeString(f.Digest)
	return err == nil
}

// Advance the durable floor before exposing any routing authority in memory.
func (s *outpostStateStore) save(floor outpostConfigFloor) error {
	raw, err := json.Marshal(floor)
	if err != nil {
		return err
	}
	name := s.name + ".next-" + uuid.NewString()
	file, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = s.root.Remove(name) }()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := s.root.Rename(name, s.name); err != nil {
		return err
	}
	directory, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

func (s *outpostStateStore) close() {
	if s == nil {
		return
	}
	if s.lock != nil {
		_ = s.lock.Unlock()
	}
	if s.root != nil {
		_ = s.root.Close()
	}
}

func validOutpostControlFloor(managed bool, c *outpostControlFloor) bool {
	return c == nil || managed && c.Generation > 0 && !c.IssuedAt.IsZero() && (c.Action == "serve" || c.Action == "drain")
}
