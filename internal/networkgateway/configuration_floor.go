package networkgateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type gatewayConfigurationFloor struct {
	Version              int    `json:"version"`
	RuntimeID            string `json:"runtimeId"`
	ControlURL           string `json:"controlUrl"`
	PublicKey            string `json:"publicKey"`
	ConfigurationVersion int    `json:"configurationVersion"`
	PolicyVersion        int    `json:"policyVersion"`
	Digest               string `json:"digest"`
}

func (r *Runtime) ConfigureState(path, controlURL, publicKey string) error {
	if !filepath.IsAbs(path) || controlURL == "" || publicKey == "" {
		return errors.New("gateway state identity is required")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("gateway state directory must be private")
	}
	r.floorPath = path
	r.floor = gatewayConfigurationFloor{Version: 1, RuntimeID: r.runtimeID, ControlURL: controlURL, PublicKey: publicKey}
	raw, err := securefile.Read(path, 8192, true)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var floor gatewayConfigurationFloor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&floor) != nil || decoder.Decode(&struct{}{}) != io.EOF || floor.Version != 1 || floor.RuntimeID != r.runtimeID || floor.ControlURL != controlURL || floor.PublicKey != publicKey || floor.ConfigurationVersion < 1 || floor.PolicyVersion < 1 || !sha256Pattern.MatchString(floor.Digest) {
		return errors.New("gateway configuration floor is invalid")
	}
	r.floor = floor
	return nil
}

func (r *Runtime) rememberConfiguration(desired networkprotocol.ConfigurationDesired) error {
	identity := desired
	// Core attaches current polling/heartbeat settings after loading the cached
	// enforcement document. They cannot change routes, policy or lease expiry.
	identity.RuntimeIntervals = nil
	raw, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	if desired.ConfigurationVersion < r.floor.ConfigurationVersion || desired.PolicyVersion < r.floor.PolicyVersion {
		return errors.New("gateway configuration regressed")
	}
	if desired.ConfigurationVersion == r.floor.ConfigurationVersion && r.floor.ConfigurationVersion > 0 {
		if digest != r.floor.Digest {
			return errors.New("gateway configuration changed without a new version")
		}
		return nil
	}
	floor := r.floor
	floor.ConfigurationVersion, floor.PolicyVersion, floor.Digest = desired.ConfigurationVersion, desired.PolicyVersion, digest
	if r.floorPath != "" {
		raw, err := json.Marshal(floor)
		if err != nil {
			return err
		}
		if err := writeGatewayFloor(r.floorPath, raw); err != nil {
			return err
		}
	}
	r.floor = floor
	return nil
}

func writeGatewayFloor(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".gateway-floor-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
