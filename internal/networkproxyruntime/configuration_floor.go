package networkproxyruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type configurationFloor struct {
	Version       int    `json:"version"`
	RuntimeID     string `json:"runtimeId"`
	ControlURL    string `json:"controlUrl"`
	Engine        string `json:"engine"`
	Revision      int64  `json:"revision"`
	PolicyVersion int    `json:"policyVersion"`
	Digest        string `json:"digest"`
	Enabled       bool   `json:"enabled"`
}

var configurationDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (r *Runner) restoreConfigurationFloor() error {
	raw, err := securefile.Read(filepath.Join(r.config.StateDir, "configuration-floor.json"), 4096, true)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read proxy configuration floor: %w", err)
	}
	var floor configurationFloor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&floor); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid proxy configuration floor")
	}
	if floor.Version != 1 || floor.RuntimeID != r.config.RuntimeID || floor.ControlURL != r.client.origin || floor.Engine != r.config.Engine || floor.Revision < 1 || floor.PolicyVersion < 0 || !configurationDigestPattern.MatchString(floor.Digest) {
		return errors.New("proxy configuration floor identity or version is invalid")
	}
	r.seenRevision, r.seenHash, r.seenEnabled, r.seenPolicyVersion = floor.Revision, floor.Digest, floor.Enabled, floor.PolicyVersion
	return nil
}

// Commit before touching the engine, including rejected replacements: a restart
// must not resurrect the predecessor of a newer, narrower configuration.
func (r *Runner) rememberConfiguration(desired domain.Configuration) error {
	policyVersion := r.seenPolicyVersion
	if desired.Protection != nil {
		if desired.Protection.PolicyVersion < policyVersion {
			return errors.New("proxy policy version regressed")
		}
		policyVersion = desired.Protection.PolicyVersion
	}
	digest := desired.Digest()
	if desired.Revision == r.seenRevision && digest == r.seenHash && desired.Enabled == r.seenEnabled && policyVersion == r.seenPolicyVersion {
		return nil
	}
	floor := configurationFloor{Version: 1, RuntimeID: r.config.RuntimeID, ControlURL: r.client.origin, Engine: r.config.Engine,
		Revision: desired.Revision, PolicyVersion: policyVersion, Digest: digest, Enabled: desired.Enabled}
	raw, err := json.Marshal(floor)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(r.config.StateDir, "configuration-floor.json"), raw); err != nil {
		return fmt.Errorf("commit proxy configuration floor: %w", err)
	}
	r.seenRevision, r.seenHash, r.seenEnabled, r.seenPolicyVersion = floor.Revision, floor.Digest, floor.Enabled, floor.PolicyVersion
	return nil
}
