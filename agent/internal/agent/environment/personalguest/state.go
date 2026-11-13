package personalguest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
	"github.com/opensoha/soha-agent/internal/agent/config"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

type state struct {
	Binding              *guest.Binding       `json:"binding"`
	Sequence             int64                `json:"sequence"`
	EnrollmentAttempted  bool                 `json:"enrollmentAttempted"`
	ActiveCertificatePEM string               `json:"activeCertificatePEM,omitempty"`
	Renewal              *guest.Renewal       `json:"renewal,omitempty"`
	RenewalResult        *guest.RenewalResult `json:"renewalResult,omitempty"`
}

type stateStore struct {
	root *os.Root
	name string
	lock *flock.Flock
}

func openState(path string) (*stateStore, state, bool, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, state{}, false, err
	}
	store := &stateStore{root: root, name: filepath.Base(path), lock: flock.New(path + ".lock")}
	closeError := func(err error) (*stateStore, state, bool, error) {
		_ = store.Close()
		return nil, state{}, false, err
	}
	lockFile, err := root.OpenFile(store.name+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_ = lockFile.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return closeError(err)
	}
	if _, err := config.ReadPrivateFile(path+".lock", 64); err != nil {
		return closeError(err)
	}
	locked, err := store.lock.TryLock()
	if err != nil || !locked {
		return closeError(errors.New("another personal guest owns the durable state"))
	}
	raw, err := config.ReadPrivateFile(path, 16384)
	if errors.Is(err, os.ErrNotExist) {
		if err := store.save(state{}); err != nil {
			return closeError(err)
		}
		return store, state{}, true, nil
	}
	if err != nil {
		return closeError(err)
	}
	var current state
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&current) != nil || decoder.Decode(new(any)) != io.EOF || current.Sequence < 0 || current.Sequence > guest.MaxCounter || current.Binding == nil && current.Sequence != 0 || current.Binding != nil && !current.EnrollmentAttempted {
		return closeError(errors.New("invalid personal guest durable state"))
	}
	return store, current, false, nil
}

// Persist before sending: uncertain replies never reuse a sequence after restart.
func (s *stateStore) save(value state) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	name := s.name + ".next-" + uuid.NewString()
	file, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
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

func (s *stateStore) Close() error {
	if s.lock != nil {
		_ = s.lock.Unlock()
	}
	return s.root.Close()
}
