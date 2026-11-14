package sohacli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	skillInstallStateSchema = "opensoha.dev/skills-install-state/v1"
	skillAuditEventSchema   = "opensoha.dev/skills-install-audit-event/v1"
	skillLockTimeout        = 30 * time.Second
	skillLockStaleAfter     = 10 * time.Minute
)

type skillInstallState struct {
	SchemaVersion string           `json:"schemaVersion"`
	Active        *skillGeneration `json:"active,omitempty"`
	Previous      *skillGeneration `json:"previous,omitempty"`
}

type skillGeneration struct {
	ID             string   `json:"id"`
	PackageVersion string   `json:"packageVersion"`
	PackageSHA256  string   `json:"packageSha256"`
	ContentSHA256  string   `json:"contentSha256"`
	SourceKind     string   `json:"sourceKind"`
	SourceURI      string   `json:"sourceUri"`
	Skills         []string `json:"skills"`
	ActivatedAt    string   `json:"activatedAt"`
}

type skillStatus struct {
	Scope           string           `json:"scope"`
	Destination     string           `json:"destination"`
	Managed         bool             `json:"managed"`
	Drifted         bool             `json:"drifted"`
	InstalledSkills []string         `json:"installedSkills"`
	Active          *skillGeneration `json:"active,omitempty"`
	Previous        *skillGeneration `json:"previous,omitempty"`
}

type skillAuditEvent struct {
	SchemaVersion  string           `json:"schemaVersion"`
	EventID        string           `json:"eventId"`
	EventType      string           `json:"eventType"`
	OccurredAt     string           `json:"occurredAt"`
	AssetType      string           `json:"assetType"`
	AssetID        string           `json:"assetId"`
	AssetVersion   string           `json:"assetVersion"`
	PackageVersion string           `json:"packageVersion"`
	Actor          skillAuditActor  `json:"actor"`
	Source         skillAuditSource `json:"source"`
	Checksum       string           `json:"checksum"`
	Decision       string           `json:"decision"`
	Reason         string           `json:"reason,omitempty"`
}

type skillAuditActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type skillAuditSource struct {
	Kind string `json:"kind"`
	URI  string `json:"uri"`
}

func runSkillStatus(args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("skill status", args, rt)
	dest := fs.String("dest", "", "destination skill directory")
	scope := fs.String("scope", "user", "installation scope: user or project")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("skill status does not accept positional arguments")
	}
	installDest, err := resolveSkillInstallDestination(*scope, *dest)
	if err != nil {
		return err
	}
	status, err := loadSkillStatus(strings.ToLower(strings.TrimSpace(*scope)), installDest)
	if err != nil {
		return err
	}
	if *jsonOutput {
		raw, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(rt.Out, string(raw))
		return err
	}
	out := newCheckedWriter(rt.Out)
	out.Printf("Skills destination: %s\n", status.Destination)
	if !status.Managed {
		out.Printf("State: unmanaged (%d installed skills)\n", len(status.InstalledSkills))
		return out.Err()
	}
	out.Printf("Active: %s (%s)\n", status.Active.PackageVersion, strings.Join(status.Active.Skills, ", "))
	if status.Previous != nil {
		out.Printf("Previous: %s\n", status.Previous.PackageVersion)
	}
	if status.Drifted {
		out.Println("Integrity: drifted")
	} else {
		out.Println("Integrity: verified")
	}
	return out.Err()
}

func runSkillUpdate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("skill update", args, rt)
	source := fs.String("source", defaultSkillSourcePath(), "skill directory, release archive, URL, or github:owner/repo[@latest|@version]")
	dest := fs.String("dest", "", "destination skill directory")
	scope := fs.String("scope", "user", "installation scope: user or project")
	all := fs.Bool("all", false, "update all source skills")
	if err := fs.Parse(args); err != nil {
		return err
	}
	installDest, err := resolveSkillInstallDestination(*scope, *dest)
	if err != nil {
		return err
	}
	state, err := readSkillInstallState(installDest)
	if err != nil {
		return err
	}
	if state.Active == nil {
		return fmt.Errorf("no managed skills installation exists at %s; run skill install first", installDest)
	}
	resolvedSource, err := resolveSkillSource(ctx, *source, rt)
	if err != nil {
		return err
	}
	names := fs.Args()
	if *all {
		names, err = listLocalSkills(resolvedSource)
		if err != nil {
			return err
		}
	}
	if len(names) == 0 {
		names = append([]string(nil), state.Active.Skills...)
	}
	generation, changed, err := installSkillGeneration(resolvedSource, *source, installDest, names, true, "upgrade")
	if err != nil {
		return err
	}
	if !changed {
		_, err = fmt.Fprintf(rt.Out, "Skills already up to date at %s\n", generation.PackageVersion)
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Updated skills to %s in %s\n", generation.PackageVersion, installDest)
	return err
}

func runSkillRemove(args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("skill remove", args, rt)
	dest := fs.String("dest", "", "destination skill directory")
	scope := fs.String("scope", "user", "installation scope: user or project")
	if err := fs.Parse(args); err != nil {
		return err
	}
	names := fs.Args()
	if len(names) == 0 {
		return fmt.Errorf("skill remove requires at least one skill id")
	}
	installDest, err := resolveSkillInstallDestination(*scope, *dest)
	if err != nil {
		return err
	}
	generation, err := removeSkillGeneration(installDest, names)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Removed %s; %d managed skills remain in %s\n", strings.Join(names, ", "), len(generation.Skills), installDest)
	return err
}

func runSkillRollback(args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("skill rollback", args, rt)
	dest := fs.String("dest", "", "destination skill directory")
	scope := fs.String("scope", "user", "installation scope: user or project")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("skill rollback does not accept positional arguments")
	}
	installDest, err := resolveSkillInstallDestination(*scope, *dest)
	if err != nil {
		return err
	}
	generation, err := rollbackSkillGeneration(installDest)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Rolled back skills to %s in %s\n", generation.PackageVersion, installDest)
	return err
}

func installSkillGeneration(source, sourceRef, dest string, names []string, overwrite bool, eventType string) (skillGeneration, bool, error) {
	unlock, err := acquireSkillInstallLock(dest)
	if err != nil {
		return skillGeneration{}, false, err
	}
	defer unlock()
	names = sortedUniqueSkillNames(names)
	stateRoot := skillStateRoot(dest)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return skillGeneration{}, false, err
	}
	stage, err := os.MkdirTemp(stateRoot, ".stage-")
	if err != nil {
		return skillGeneration{}, false, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := copyDirectoryContents(dest, stage); err != nil && !errors.Is(err, os.ErrNotExist) {
		return skillGeneration{}, false, err
	}
	for _, name := range names {
		if !isSafeSkillName(name) {
			return skillGeneration{}, false, fmt.Errorf("invalid skill id %q", name)
		}
		sourceDir := filepath.Join(source, name)
		if _, err := os.Stat(filepath.Join(sourceDir, "SKILL.md")); err != nil {
			return skillGeneration{}, false, err
		}
		targetDir := filepath.Join(stage, name)
		if _, err := os.Stat(targetDir); err == nil {
			if !overwrite {
				return skillGeneration{}, false, fmt.Errorf("skill %q already exists at %s; pass --overwrite to replace it", name, filepath.Join(dest, name, "SKILL.md"))
			}
			if err := os.RemoveAll(targetDir); err != nil {
				return skillGeneration{}, false, err
			}
		}
		if err := copyDirectory(sourceDir, targetDir); err != nil {
			return skillGeneration{}, false, err
		}
	}
	installed, err := listLocalSkills(stage)
	if err != nil {
		return skillGeneration{}, false, err
	}
	packageVersion, packageSHA, sourceKind := skillSourceMetadata(source)
	contentSHA, err := hashDirectory(stage)
	if err != nil {
		return skillGeneration{}, false, err
	}
	if packageSHA == "" {
		packageSHA = contentSHA
	}
	now := time.Now().UTC()
	generation := skillGeneration{
		ID:             skillGenerationID(packageVersion, contentSHA, now),
		PackageVersion: packageVersion,
		PackageSHA256:  packageSHA,
		ContentSHA256:  contentSHA,
		SourceKind:     sourceKind,
		SourceURI:      normalizedSourceURI(sourceRef),
		Skills:         installed,
		ActivatedAt:    now.Format(time.RFC3339Nano),
	}
	state, err := readSkillInstallState(dest)
	if err != nil {
		return skillGeneration{}, false, err
	}
	if state.Active != nil && state.Active.ContentSHA256 == generation.ContentSHA256 && state.Active.PackageSHA256 == generation.PackageSHA256 {
		return *state.Active, false, nil
	}
	if err := appendSkillAudit(dest, generation, "verify", "allowed", "validated staged skills generation"); err != nil {
		return skillGeneration{}, false, err
	}
	if err := activateSkillStage(dest, stage, &state, generation, eventType, "allowed", ""); err != nil {
		return skillGeneration{}, false, err
	}
	return generation, true, nil
}

func removeSkillGeneration(dest string, names []string) (skillGeneration, error) {
	unlock, err := acquireSkillInstallLock(dest)
	if err != nil {
		return skillGeneration{}, err
	}
	defer unlock()
	names = sortedUniqueSkillNames(names)
	state, err := readSkillInstallState(dest)
	if err != nil {
		return skillGeneration{}, err
	}
	if state.Active == nil {
		return skillGeneration{}, fmt.Errorf("no managed skills installation exists at %s", dest)
	}
	remove := make(map[string]bool, len(names))
	active := make(map[string]bool, len(state.Active.Skills))
	for _, name := range state.Active.Skills {
		active[name] = true
	}
	for _, name := range names {
		if !isSafeSkillName(name) || !active[name] {
			return skillGeneration{}, fmt.Errorf("skill %q is not installed", name)
		}
		remove[name] = true
	}
	stateRoot := skillStateRoot(dest)
	stage, err := os.MkdirTemp(stateRoot, ".stage-")
	if err != nil {
		return skillGeneration{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := copyDirectoryContents(dest, stage); err != nil {
		return skillGeneration{}, err
	}
	for name := range remove {
		if err := os.RemoveAll(filepath.Join(stage, name)); err != nil {
			return skillGeneration{}, err
		}
	}
	remaining, err := listLocalSkills(stage)
	if err != nil {
		return skillGeneration{}, err
	}
	contentSHA, err := hashDirectory(stage)
	if err != nil {
		return skillGeneration{}, err
	}
	now := time.Now().UTC()
	generation := *state.Active
	generation.ID = skillGenerationID(generation.PackageVersion, contentSHA, now)
	generation.ContentSHA256 = contentSHA
	generation.Skills = remaining
	generation.ActivatedAt = now.Format(time.RFC3339Nano)
	if err := activateSkillStage(dest, stage, &state, generation, "activate", "allowed", "removed skills: "+strings.Join(names, ", ")); err != nil {
		return skillGeneration{}, err
	}
	return generation, nil
}

func rollbackSkillGeneration(dest string) (skillGeneration, error) {
	unlock, err := acquireSkillInstallLock(dest)
	if err != nil {
		return skillGeneration{}, err
	}
	defer unlock()
	state, err := readSkillInstallState(dest)
	if err != nil {
		return skillGeneration{}, err
	}
	if state.Active == nil || state.Previous == nil {
		return skillGeneration{}, fmt.Errorf("no previous managed skills generation exists at %s", dest)
	}
	stateRoot := skillStateRoot(dest)
	versionsDir := filepath.Join(stateRoot, "versions")
	previousDir := filepath.Join(versionsDir, state.Previous.ID)
	previousSHA, err := hashDirectory(previousDir)
	if err != nil {
		return skillGeneration{}, fmt.Errorf("verify previous skills generation: %w", err)
	}
	if previousSHA != state.Previous.ContentSHA256 {
		return skillGeneration{}, fmt.Errorf("previous skills generation failed integrity verification")
	}
	currentDir := filepath.Join(versionsDir, state.Active.ID)
	if _, err := os.Stat(currentDir); err == nil {
		return skillGeneration{}, fmt.Errorf("skills generation path already exists: %s", currentDir)
	}
	if err := os.Rename(dest, currentDir); err != nil {
		return skillGeneration{}, err
	}
	if err := os.Rename(previousDir, dest); err != nil {
		_ = os.Rename(currentDir, dest)
		return skillGeneration{}, err
	}
	oldState := state
	oldActive := state.Active
	state.Active = state.Previous
	state.Previous = oldActive
	if err := writeSkillInstallState(dest, state); err != nil {
		_ = os.Rename(dest, previousDir)
		_ = os.Rename(currentDir, dest)
		return skillGeneration{}, err
	}
	if err := appendSkillAudit(dest, *state.Active, "rollback", "rolledBack", "restored previous verified generation"); err != nil {
		_ = os.Rename(dest, previousDir)
		_ = os.Rename(currentDir, dest)
		_ = writeSkillInstallState(dest, oldState)
		return skillGeneration{}, err
	}
	return *state.Active, nil
}

func activateSkillStage(dest, stage string, state *skillInstallState, generation skillGeneration, eventType, decision, reason string) error {
	stateRoot := skillStateRoot(dest)
	versionsDir := filepath.Join(stateRoot, "versions")
	if err := os.MkdirAll(versionsDir, 0o700); err != nil {
		return err
	}
	var previous *skillGeneration
	var previousDir string
	if info, err := os.Stat(dest); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("skill destination is not a directory: %s", dest)
		}
		previous = state.Active
		if previous == nil {
			legacy, err := legacySkillGeneration(dest)
			if err != nil {
				return err
			}
			previous = &legacy
		}
		previousDir = filepath.Join(versionsDir, previous.ID)
		if _, err := os.Stat(previousDir); err == nil {
			return fmt.Errorf("skills generation path already exists: %s", previousDir)
		}
		if err := os.Rename(dest, previousDir); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		if previousDir != "" {
			_ = os.Rename(previousDir, dest)
		}
		return err
	}
	if err := os.Rename(stage, dest); err != nil {
		if previousDir != "" {
			_ = os.Rename(previousDir, dest)
		}
		return err
	}
	oldState := *state
	state.SchemaVersion = skillInstallStateSchema
	state.Active = &generation
	state.Previous = previous
	if err := writeSkillInstallState(dest, *state); err != nil {
		_ = os.Rename(dest, stage)
		if previousDir != "" {
			_ = os.Rename(previousDir, dest)
		}
		*state = oldState
		return err
	}
	if err := appendSkillAudit(dest, generation, eventType, decision, reason); err != nil {
		_ = os.Rename(dest, stage)
		if previousDir != "" {
			_ = os.Rename(previousDir, dest)
		}
		*state = oldState
		if oldState.Active == nil && oldState.Previous == nil {
			_ = os.Remove(filepath.Join(skillStateRoot(dest), "state.json"))
		} else {
			_ = writeSkillInstallState(dest, oldState)
		}
		return err
	}
	return nil
}

func loadSkillStatus(scope, dest string) (skillStatus, error) {
	state, err := readSkillInstallState(dest)
	if err != nil {
		return skillStatus{}, err
	}
	installed, err := listLocalSkills(dest)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return skillStatus{}, err
	}
	status := skillStatus{Scope: scope, Destination: dest, InstalledSkills: installed, Active: state.Active, Previous: state.Previous}
	if state.Active == nil {
		return status, nil
	}
	status.Managed = true
	contentSHA, err := hashDirectory(dest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Drifted = true
			return status, nil
		}
		return skillStatus{}, err
	}
	status.Drifted = contentSHA != state.Active.ContentSHA256
	return status, nil
}

func readSkillInstallState(dest string) (skillInstallState, error) {
	path := filepath.Join(skillStateRoot(dest), "state.json")
	// #nosec G304 -- path is derived from the managed skill state root.
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return skillInstallState{SchemaVersion: skillInstallStateSchema}, nil
	}
	if err != nil {
		return skillInstallState{}, err
	}
	var state skillInstallState
	if err := json.Unmarshal(raw, &state); err != nil {
		return skillInstallState{}, fmt.Errorf("decode skills state: %w", err)
	}
	if state.SchemaVersion != skillInstallStateSchema {
		return skillInstallState{}, fmt.Errorf("unsupported skills state schema %q", state.SchemaVersion)
	}
	return state, nil
}

func writeSkillInstallState(dest string, state skillInstallState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(filepath.Join(skillStateRoot(dest), "state.json"), append(raw, '\n'), 0o600)
}

func appendSkillAudit(dest string, generation skillGeneration, eventType, decision, reason string) error {
	now := time.Now().UTC()
	actorID := strings.TrimSpace(os.Getenv("USER"))
	if actorID == "" {
		actorID = "local-user"
	}
	checksum := generation.PackageSHA256
	if checksum == "" {
		checksum = generation.ContentSHA256
	}
	event := skillAuditEvent{
		SchemaVersion:  skillAuditEventSchema,
		EventID:        fmt.Sprintf("skill-%x", now.UnixNano()),
		EventType:      eventType,
		OccurredAt:     now.Format(time.RFC3339Nano),
		AssetType:      "package",
		AssetID:        "soha-skills",
		AssetVersion:   generation.PackageVersion,
		PackageVersion: generation.PackageVersion,
		Actor:          skillAuditActor{Type: "user", ID: actorID},
		Source:         skillAuditSource{Kind: generation.SourceKind, URI: generation.SourceURI},
		Checksum:       "sha256:" + checksum,
		Decision:       decision,
		Reason:         reason,
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	path := filepath.Join(skillStateRoot(dest), "audit.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// #nosec G304 -- path is derived from the managed skill state root.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func skillSourceMetadata(source string) (version, checksum, kind string) {
	kind = "localDirectory"
	version = "0.0.0-local"
	current := filepath.Clean(source)
	for range 10 {
		raw, err := os.ReadFile(filepath.Join(current, ".verified.json"))
		if err == nil {
			var marker skillCacheMarker
			if json.Unmarshal(raw, &marker) == nil && marker.SchemaVersion == skillsCacheMarkerSchema && semverPattern.MatchString(marker.Version) && sha256Pattern.MatchString(marker.SHA256) {
				return marker.Version, marker.SHA256, "releasePackage"
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return version, checksum, kind
}

func legacySkillGeneration(dest string) (skillGeneration, error) {
	skills, err := listLocalSkills(dest)
	if err != nil {
		return skillGeneration{}, err
	}
	contentSHA, err := hashDirectory(dest)
	if err != nil {
		return skillGeneration{}, err
	}
	now := time.Now().UTC()
	return skillGeneration{
		ID:             skillGenerationID("0.0.0-legacy", contentSHA, now),
		PackageVersion: "0.0.0-legacy",
		PackageSHA256:  contentSHA,
		ContentSHA256:  contentSHA,
		SourceKind:     "localDirectory",
		SourceURI:      dest,
		Skills:         skills,
		ActivatedAt:    now.Format(time.RFC3339Nano),
	}, nil
}

func skillStateRoot(dest string) string {
	dest = filepath.Clean(dest)
	return filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".soha-state")
}

func acquireSkillInstallLock(dest string) (func(), error) {
	root := skillStateRoot(dest)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ".lock")
	deadline := time.Now().Add(skillLockTimeout)
	for {
		// #nosec G304 -- path is the lock file under the managed skill state root.
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid())
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(path)
				return nil, errors.Join(writeErr, closeErr)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > skillLockStaleAfter {
			if removeErr := os.Remove(path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for skills installation lock %s", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func skillGenerationID(version, checksum string, now time.Time) string {
	version = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			return r
		}
		return '-'
	}, version)
	return fmt.Sprintf("%s-%s-%x", version, checksum[:12], now.UnixNano())
}

func normalizedSourceURI(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "local"
	}
	if info, err := os.Stat(source); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
		if absolute, err := filepath.Abs(source); err == nil {
			return absolute
		}
	}
	return source
}

func hashDirectory(root string) (string, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer func() { _ = directory.Close() }()
	hash := sha256.New()
	err = fs.WalkDir(directory.FS(), ".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file in skills directory: %s", path)
		}
		_, _ = io.WriteString(hash, path)
		_, _ = hash.Write([]byte{0})
		file, err := directory.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyDirectoryContents(source, dest string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyDirectoryEntry(filepath.Join(source, entry.Name()), filepath.Join(dest, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyDirectory(source, dest string) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	return copyDirectoryContents(source, dest)
}

func copyDirectoryEntry(source, dest string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirectory(source, dest)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file in skills directory: %s", source)
	}
	// #nosec G304 -- source is derived from a validated directory entry in the skills tree.
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info.Mode()&0o100 != 0 {
		mode = 0o700
	}
	// #nosec G304 -- dest is derived from the validated destination root and source entry name.
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func writeAtomicFile(path string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if mode.Perm() == 0o600 {
		if err := restrictPrivateFile(tempPath); err != nil {
			_ = file.Close()
			return err
		}
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
	return atomicReplaceFile(tempPath, path)
}

func sortedUniqueSkillNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
