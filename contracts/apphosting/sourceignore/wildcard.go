package sourceignore

import (
	"errors"
	"regexp"
	"strings"
)

func compileComponent(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, errors.New("empty wildcard component")
	}
	var expression strings.Builder
	expression.WriteString("(?s)^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteString(".")
		case '\\':
			i++
			if i == len(pattern) {
				return nil, errors.New("invalid wildcard escape")
			}
			expression.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		case '[':
			end, err := appendClass(&expression, pattern, i+1)
			if err != nil {
				return nil, err
			}
			i = end
		default:
			expression.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	expression.WriteByte('$')
	return regexp.Compile(expression.String())
}

func appendClass(expression *strings.Builder, pattern string, start int) (int, error) {
	i := start
	expression.WriteByte('[')
	if i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^') {
		expression.WriteByte('^')
		i++
	}
	if i < len(pattern) && pattern[i] == ']' {
		expression.WriteString(`\]`)
		i++
	}
	for i < len(pattern) {
		switch pattern[i] {
		case ']':
			expression.WriteByte(']')
			return i, nil
		case '\\':
			i++
			if i == len(pattern) {
				return 0, errors.New("invalid escape in character class")
			}
			if strings.ContainsRune(`\[]^-`, rune(pattern[i])) {
				expression.WriteByte('\\')
			}
		case '[':
			if strings.HasPrefix(pattern[i:], "[:") {
				end := strings.Index(pattern[i+2:], ":]")
				if end == -1 {
					return 0, errors.New("unclosed POSIX character class")
				}
				expression.WriteString(pattern[i : i+end+4])
				i += end + 4
				continue
			}
			expression.WriteByte('\\')
		}
		expression.WriteByte(pattern[i])
		i++
	}
	return 0, errors.New("unclosed character class")
}
