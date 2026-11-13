package sohacli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultSkillSource          = "skills/ai-gateway"
	defaultPublishedSkillSource = "github:opensoha/soha-skills"
)

func runSkill(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("skill requires a subcommand: list, install, status, update, remove, or rollback")
	}
	switch args[0] {
	case "list":
		return runSkillList(ctx, args[1:], rt)
	case "install":
		return runSkillInstall(ctx, args[1:], rt)
	case "status":
		return runSkillStatus(args[1:], rt)
	case "update":
		return runSkillUpdate(ctx, args[1:], rt)
	case "remove":
		return runSkillRemove(args[1:], rt)
	case "rollback":
		return runSkillRollback(args[1:], rt)
	default:
		return fmt.Errorf("unknown skill command %q", args[0])
	}
}

func runSkillList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("skill list", args, rt)
	source := fs.String("source", defaultSkillSourcePath(), "skill directory, release archive, URL, or github:owner/repo[@latest|@version]")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedSource, err := resolveSkillSource(ctx, *source, rt)
	if err != nil {
		return err
	}
	items, err := listLocalSkills(resolvedSource)
	if err != nil {
		return err
	}
	out := newCheckedWriter(rt.Out)
	for _, item := range items {
		out.Println(item)
	}
	return out.Err()
}

func runSkillInstall(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("skill install", args, rt)
	source := fs.String("source", defaultSkillSourcePath(), "skill directory, release archive, URL, or github:owner/repo[@latest|@version]")
	dest := fs.String("dest", "", "destination skill directory")
	scope := fs.String("scope", "user", "installation scope: user or project")
	all := fs.Bool("all", false, "install all source skills")
	overwrite := fs.Bool("overwrite", false, "overwrite existing installed skill files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	installDest, err := resolveSkillInstallDestination(*scope, *dest)
	if err != nil {
		return err
	}
	resolvedSource, err := resolveSkillSource(ctx, *source, rt)
	if err != nil {
		return err
	}
	names := fs.Args()
	if *all {
		items, err := listLocalSkills(resolvedSource)
		if err != nil {
			return err
		}
		names = items
	}
	if len(names) == 0 {
		return fmt.Errorf("skill install requires a skill id or --all")
	}
	generation, changed, err := installSkillGeneration(resolvedSource, *source, installDest, names, *overwrite, "install")
	if err != nil {
		return err
	}
	if !changed {
		_, err = fmt.Fprintf(rt.Out, "Skills already up to date in %s\n", installDest)
		return err
	}
	out := newCheckedWriter(rt.Out)
	for _, name := range generation.Skills {
		out.Printf("Installed skill %s to %s\n", name, filepath.Join(installDest, name, "SKILL.md"))
	}
	return out.Err()
}

func defaultSkillSourcePath() string {
	if value := env("SOHA_SKILLS_SOURCE"); value != "" {
		return value
	}
	return defaultPublishedSkillSource
}

func defaultSkillInstallPath() string {
	if value := env("SOHA_SKILLS_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".soha", "skills")
	}
	return filepath.Join(home, ".soha", "skills")
}

func resolveSkillInstallDestination(scope, explicit string) (string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "user" && scope != "project" {
		return "", fmt.Errorf("invalid --scope %q; use user or project", scope)
	}
	if strings.TrimSpace(explicit) != "" {
		return expandHome(explicit), nil
	}
	if scope == "user" {
		return defaultSkillInstallPath(), nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	return filepath.Join(workingDir, ".soha", "skills"), nil
}

func listLocalSkills(source string) ([]string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("skill source directory is required")
	}
	source = normalizeSkillSourcePath(source)
	entries, err := os.ReadDir(source)
	if err != nil {
		return nil, err
	}
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isSafeSkillName(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(source, name, "SKILL.md")); err == nil {
			items = append(items, name)
		}
	}
	sort.Strings(items)
	return items, nil
}

func normalizeSkillSourcePath(source string) string {
	source = strings.TrimSpace(source)
	for _, candidate := range []string{
		source,
		filepath.Join(source, defaultSkillSource),
		filepath.Join(source, "ai-gateway"),
	} {
		if isSkillSourceDir(candidate) {
			return candidate
		}
	}
	return source
}

func isSkillSourceDir(source string) bool {
	entries, err := os.ReadDir(source)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isSafeSkillName(entry.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(source, entry.Name(), "SKILL.md")); err == nil {
			return true
		}
	}
	return false
}

func isSafeSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
