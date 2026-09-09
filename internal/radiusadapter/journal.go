package radiusadapter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/opensoha/soha/internal/networkprotocol"
)

const maxJournalBytes = 1 << 20

type Journal struct {
	path    string
	pending map[string]networkprotocol.RuntimeMessage
}

type journalFile struct {
	Version int                                       `json:"version"`
	Pending map[string]networkprotocol.RuntimeMessage `json:"pending"`
}

func OpenJournal(path string) (*Journal, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("RADIUS result journal path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create RADIUS result journal directory: %w", err)
	}
	journal := &Journal{path: path, pending: make(map[string]networkprotocol.RuntimeMessage)}
	file, err := os.Open(path) // #nosec G304 -- absolute journal path comes from operator configuration, not runtime messages.
	if os.IsNotExist(err) {
		return journal, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open RADIUS result journal: %w", err)
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxJournalBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read RADIUS result journal: %w", err)
	}
	if len(raw) > maxJournalBytes {
		return nil, fmt.Errorf("RADIUS result journal exceeds %d bytes", maxJournalBytes)
	}
	var stored journalFile
	if err := json.Unmarshal(raw, &stored); err != nil || stored.Version != 1 || stored.Pending == nil {
		return nil, fmt.Errorf("RADIUS result journal is invalid")
	}
	journal.pending = stored.Pending
	return journal, nil
}

func (j *Journal) Put(commandID string, message networkprotocol.RuntimeMessage) error {
	updated := clonePending(j.pending)
	updated[commandID] = message
	if err := j.persist(updated); err != nil {
		return err
	}
	j.pending = updated
	return nil
}

func (j *Journal) Delete(commandID string) error {
	if _, ok := j.pending[commandID]; !ok {
		return nil
	}
	updated := clonePending(j.pending)
	delete(updated, commandID)
	if err := j.persist(updated); err != nil {
		return err
	}
	j.pending = updated
	return nil
}

func (j *Journal) Pending() []networkprotocol.RuntimeMessage {
	keys := make([]string, 0, len(j.pending))
	for key := range j.pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]networkprotocol.RuntimeMessage, 0, len(keys))
	for _, key := range keys {
		result = append(result, j.pending[key])
	}
	return result
}

func (j *Journal) Len() int { return len(j.pending) }

func (j *Journal) persist(pending map[string]networkprotocol.RuntimeMessage) error {
	raw, err := json.Marshal(journalFile{Version: 1, Pending: pending})
	if err != nil {
		return fmt.Errorf("encode RADIUS result journal: %w", err)
	}
	if len(raw) > maxJournalBytes {
		return fmt.Errorf("RADIUS result journal exceeds %d bytes", maxJournalBytes)
	}
	temporary, err := os.CreateTemp(filepath.Dir(j.path), ".radius-results-*")
	if err != nil {
		return fmt.Errorf("create RADIUS result journal: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure RADIUS result journal: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write RADIUS result journal: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync RADIUS result journal: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close RADIUS result journal: %w", err)
	}
	if err := os.Rename(temporaryPath, j.path); err != nil {
		return fmt.Errorf("replace RADIUS result journal: %w", err)
	}
	return nil
}

func clonePending(source map[string]networkprotocol.RuntimeMessage) map[string]networkprotocol.RuntimeMessage {
	result := make(map[string]networkprotocol.RuntimeMessage, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
