// Package sourceignore applies project .gitignore files to a hosted source snapshot.
package sourceignore

import (
	"bytes"
	"errors"
	"path"
	"regexp"
	"strings"
)

const (
	MaxFileBytes = 256 << 10
	MaxPathBytes = 1024

	maxProjectBytes = 2 << 20
	maxProjectFiles = 2048
	maxRules        = 4096
	maxRuleBytes    = 4096
)

type Rules struct {
	directories map[string][]pattern
	count       int
	inputBytes  int
	inputFiles  int
}

type pattern struct {
	segments  []*regexp.Regexp
	exclude   bool
	directory bool
	baseName  bool
}

// Add reads the rules relative to directory. The project root is the empty string.
func (r *Rules) Add(directory string, contents []byte) error {
	if directory != "" && (!validPath(directory) || path.Clean(directory) != directory) {
		return errors.New("invalid ignore directory")
	}
	if len(contents) > MaxFileBytes || bytes.IndexByte(contents, 0) >= 0 {
		return errors.New("invalid or oversized .gitignore")
	}
	if len(contents) > maxProjectBytes-r.inputBytes || r.inputFiles >= maxProjectFiles {
		return errors.New("project .gitignore files exceed input limits")
	}

	var compiled []pattern
	remaining := maxRules - r.count + len(r.directories[directory])
	contents = bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})
	for raw := range bytes.SplitSeq(contents, []byte{'\n'}) {
		line := strings.TrimSuffix(string(raw), "\r")
		if len(line) > maxRuleBytes {
			return errors.New(".gitignore pattern exceeds maximum length")
		}
		line = trimUnescapedSpaces(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(compiled) >= remaining {
			return errors.New("project exceeds maximum .gitignore pattern count")
		}
		if rule, ok := parsePattern(line); ok {
			compiled = append(compiled, rule)
		}
	}
	r.inputBytes += len(contents)
	r.inputFiles++
	r.count += len(compiled) - len(r.directories[directory])
	if len(compiled) == 0 {
		delete(r.directories, directory)
	} else {
		if r.directories == nil {
			r.directories = make(map[string][]pattern)
		}
		r.directories[directory] = compiled
	}
	return nil
}

// Ignored reports whether a project-relative path is excluded. A rule in an
// excluded parent directory cannot make any of that parent's children visible.
func (r *Rules) Ignored(relative string, isDir bool) bool {
	if r.count == 0 || !validPath(relative) {
		return false
	}
	components := strings.Split(relative, "/")
	scopes := []scopedRules{{patterns: r.directories[""]}}
	length := 0
	for depth, component := range components {
		directory := isDir || depth < len(components)-1
		if excludedByScopes(scopes, components[:depth+1], directory) {
			return true
		}
		length += len(component)
		if patterns := r.directories[relative[:length]]; len(patterns) > 0 {
			scopes = append(scopes, scopedRules{start: depth + 1, patterns: patterns})
		}
		length++
	}
	return false
}

type scopedRules struct {
	patterns []pattern
	start    int
}

func excludedByScopes(scopes []scopedRules, components []string, isDir bool) bool {
	excluded := false
	for _, scope := range scopes {
		for _, rule := range scope.patterns {
			if rule.matches(components[scope.start:], isDir) {
				excluded = rule.exclude
			}
		}
	}
	return excluded
}

func validPath(relative string) bool {
	if relative == "" || len(relative) > MaxPathBytes || strings.ContainsAny(relative, "\\\x00") {
		return false
	}
	for part := range strings.SplitSeq(relative, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func parsePattern(line string) (pattern, bool) {
	raw, negated := strings.CutPrefix(line, "!")
	raw, directory := strings.CutSuffix(raw, "/")
	raw, anchored := strings.CutPrefix(raw, "/")
	raw = normalizeEscapedSeparators(raw)
	if raw == "" {
		return pattern{}, false
	}
	p := pattern{exclude: !negated, directory: directory, baseName: !anchored && !strings.Contains(raw, "/")}
	for part := range strings.SplitSeq(raw, "/") {
		if part == "**" && !p.baseName {
			p.segments = append(p.segments, nil)
			continue
		}
		matcher, err := compileComponent(part)
		if err != nil {
			// As with Git, invalid wildcard patterns match nothing.
			return pattern{}, false
		}
		p.segments = append(p.segments, matcher)
	}
	return p, true
}

// Interpret escapes around separators before splitting the pattern into components.
func normalizeEscapedSeparators(line string) string {
	if !strings.Contains(line, `\/`) {
		return line
	}
	var normalized strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != '\\' || i+1 == len(line) {
			normalized.WriteByte(line[i])
			continue
		}
		i++
		if line[i] != '/' {
			normalized.WriteByte('\\')
		}
		normalized.WriteByte(line[i])
	}
	return normalized.String()
}

func trimUnescapedSpaces(line string) string {
	for len(line) > 0 && line[len(line)-1] == ' ' {
		last := len(line) - 1
		i := last
		for i > 0 && line[i-1] == '\\' {
			i--
		}
		if (last-i)%2 != 0 {
			break
		}
		line = line[:last]
	}
	return line
}

func (p pattern) matches(components []string, isDir bool) bool {
	if p.directory && !isDir {
		return false
	}
	if p.baseName {
		return p.segments[0].MatchString(components[len(components)-1])
	}

	if !p.hasGlobstar() {
		return p.matchesFixedPath(components)
	}

	// The index in matched is the number of path components already consumed.
	matched := make([]bool, len(components)+1)
	matched[0] = true
	for i, segment := range p.segments {
		if segment == nil {
			if i == len(p.segments)-1 && i > 0 {
				// A trailing /** denotes the contents, excluding the directory itself.
				copy(matched[1:], matched[:len(components)])
				matched[0] = false
			}
			for j := 1; j < len(matched); j++ {
				matched[j] = matched[j] || matched[j-1]
			}
			continue
		}
		for j := len(components); j > 0; j-- {
			matched[j] = matched[j-1] && segment.MatchString(components[j-1])
		}
		matched[0] = false
	}
	return matched[len(components)]
}

func (p pattern) hasGlobstar() bool {
	for _, segment := range p.segments {
		if segment == nil {
			return true
		}
	}
	return false
}

func (p pattern) matchesFixedPath(components []string) bool {
	if len(p.segments) != len(components) {
		return false
	}
	for index, segment := range p.segments {
		if !segment.MatchString(components[index]) {
			return false
		}
	}
	return true
}
