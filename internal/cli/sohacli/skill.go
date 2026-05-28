package sohacli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultSkillSource = "skills/ai-gateway"

func runSkill(args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("skill requires a subcommand: list or install")
	}
	switch args[0] {
	case "list":
		return runSkillList(args[1:], rt)
	case "install":
		return runSkillInstall(args[1:], rt)
	default:
		return fmt.Errorf("unknown skill command %q", args[0])
	}
}

func runSkillList(args []string, rt Runtime) error {
	fs := newFlagSet("skill list", rt.Err)
	source := fs.String("source", defaultSkillSourcePath(), "source skill directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, err := listLocalSkills(*source)
	if err != nil {
		return err
	}
	for _, item := range items {
		fmt.Fprintln(rt.Out, item)
	}
	return nil
}

func runSkillInstall(args []string, rt Runtime) error {
	fs := newFlagSet("skill install", rt.Err)
	source := fs.String("source", defaultSkillSourcePath(), "source skill directory")
	dest := fs.String("dest", defaultSkillInstallPath(), "destination skill directory")
	all := fs.Bool("all", false, "install all source skills")
	overwrite := fs.Bool("overwrite", false, "overwrite existing installed skill files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	names := fs.Args()
	if *all {
		items, err := listLocalSkills(*source)
		if err != nil {
			return err
		}
		names = items
	}
	if len(names) == 0 {
		return fmt.Errorf("skill install requires a skill id or --all")
	}
	for _, name := range names {
		installed, err := installLocalSkill(*source, *dest, name, *overwrite)
		if err != nil {
			return err
		}
		fmt.Fprintf(rt.Out, "Installed skill %s to %s\n", name, installed)
	}
	return nil
}

func defaultSkillSourcePath() string {
	if value := env("SOHA_SKILLS_SOURCE"); value != "" {
		return value
	}
	return defaultSkillSource
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

func listLocalSkills(source string) ([]string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("skill source directory is required")
	}
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

func installLocalSkill(source, dest, name string, overwrite bool) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("skill source directory is required")
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", fmt.Errorf("skill destination directory is required")
	}
	name = strings.TrimSpace(name)
	if !isSafeSkillName(name) {
		return "", fmt.Errorf("invalid skill id %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(source, name, "SKILL.md"))
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(dest, name)
	targetFile := filepath.Join(targetDir, "SKILL.md")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	flag := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_EXCL
	}
	file, err := os.OpenFile(targetFile, flag, fs.FileMode(0o644))
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("skill %q already exists at %s; pass --overwrite to replace it", name, targetFile)
		}
		return "", err
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		return "", err
	}
	return targetFile, nil
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
