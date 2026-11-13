// Check independent module imports without relying on Go's shared parent prefix.
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func check(root string) error {
	for _, module := range []string{"contracts", "cli", "agent"} {
		directory := filepath.Join(root, module)
		if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("independent module directory required: %s", directory)
		}
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".tmp":
					return filepath.SkipDir
				}
				return nil
			}
			return checkFile(path, entry)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func checkFile(path string, entry fs.DirEntry) error {
	if entry.Type()&os.ModeSymlink != 0 {
		if filepath.Ext(path) != ".go" {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				return nil
			}
		}
		return fmt.Errorf("module source must not redirect: %s", path)
	}
	if filepath.Ext(path) != ".go" {
		return nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse module imports: %w", err)
	}
	for _, spec := range file.Imports {
		name, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return err
		}
		if name == "github.com/opensoha/soha/internal" || strings.HasPrefix(name, "github.com/opensoha/soha/internal/") {
			return fmt.Errorf("%s must not import Core internal package %s", path, name)
		}
	}
	return nil
}

func main() {
	root := flag.String("root", ".", "Root source checkout")
	flag.Parse()
	if err := check(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
