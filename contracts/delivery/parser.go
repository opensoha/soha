package delivery

import (
	"bytes"
	"io"
	"math"
	"math/big"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func ValidPath(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && value != "." &&
		path.Clean(value) == value && !path.IsAbs(value) && value != ".." && !strings.HasPrefix(value, "../") &&
		!strings.ContainsAny(value, "\\\x00\r\n:")
}

// Parse accepts one bounded JSON-subset YAML document, including JSON syntax.
// It retains source locations and rejects aliases before decoding any values.
func Parse(filePath string, content []byte) (Document, []Diagnostic) {
	if !ValidPath(filePath) {
		return Document{}, []Diagnostic{diagnostic(filePath, "", "invalid_path", "path must be a normalized relative file path", nil)}
	}
	if len(content) == 0 || len(content) > MaxFileBytes || !utf8.Valid(content) {
		return Document{}, []Diagnostic{diagnostic(filePath, "", "file_limit", "file must be UTF-8 and contain 1 to 1048576 bytes", nil)}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var root, trailing yaml.Node
	if err := decoder.Decode(&root); err != nil || len(root.Content) != 1 {
		return Document{}, []Diagnostic{diagnostic(filePath, "", "syntax", "invalid YAML or JSON syntax", &root)}
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Document{}, []Diagnostic{diagnostic(filePath, "", "multiple_documents", "each file must contain exactly one document", &trailing)}
	}
	parser := documentParser{path: filePath, locations: map[string]*yaml.Node{}}
	value, invalid := parser.value(root.Content[0], "", 0)
	if invalid != nil {
		return Document{}, []Diagnostic{*invalid}
	}
	return validateDocument(filePath, value, parser.locations)
}

type documentParser struct {
	path      string
	nodes     int
	locations map[string]*yaml.Node
}

func (p *documentParser) value(node *yaml.Node, pointer string, depth int) (any, *Diagnostic) {
	p.nodes++
	p.locations[pointer] = node
	if depth > 64 || p.nodes > 100000 {
		return nil, p.invalid(node, pointer, "structure_limit", "document exceeds its nesting or node limit")
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" || node.Style&yaml.TaggedStyle != 0 {
		return nil, p.invalid(node, pointer, "yaml_feature", "aliases, anchors and explicit tags are unsupported")
	}
	switch node.Kind {
	case yaml.MappingNode:
		return p.mapping(node, pointer, depth)
	case yaml.SequenceNode:
		values := make([]any, 0, len(node.Content))
		for index, child := range node.Content {
			value, err := p.value(child, pointer+"/"+strconv.Itoa(index), depth+1)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case yaml.ScalarNode:
		return p.scalar(node, pointer)
	default:
		return nil, p.invalid(node, pointer, "unsupported_value", "only JSON-compatible values are supported")
	}
}

func (p *documentParser) mapping(node *yaml.Node, pointer string, depth int) (any, *Diagnostic) {
	values := make(map[string]any, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, child := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Style&yaml.TaggedStyle != 0 || key.Anchor != "" {
			return nil, p.invalid(key, pointer, "non_string_key", "object keys must be strings; merge keys are unsupported")
		}
		childPointer := pointer + "/" + escapePointer(key.Value)
		if _, exists := values[key.Value]; exists {
			return nil, p.invalid(key, childPointer, "duplicate_key", "duplicate object key")
		}
		value, err := p.value(child, childPointer, depth+1)
		if err != nil {
			return nil, err
		}
		values[key.Value] = value
	}
	return values, nil
}

func (p *documentParser) scalar(node *yaml.Node, pointer string) (any, *Diagnostic) {
	switch node.Tag {
	case "!!str":
		return node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		return strings.EqualFold(node.Value, "true"), nil
	case "!!int", "!!float":
		if !jsonNumber.MatchString(node.Value) {
			return nil, p.invalid(node, pointer, "invalid_number", "numbers must use finite JSON decimal notation")
		}
		value, err := strconv.ParseFloat(node.Value, 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.Trunc(value) == value && math.Abs(value) > 9007199254740991 || documentNumberBoundaryLoss(node.Value, value) {
			return nil, p.invalid(node, pointer, "number_precision", "number exceeds finite IEEE-754 or safe integer limits")
		}
		return value, nil
	default:
		return nil, p.invalid(node, pointer, "unsupported_scalar", "quote dates and other non-JSON scalar values as strings")
	}
}

func documentNumberBoundaryLoss(raw string, value float64) bool {
	if value != 0 && math.Abs(value) != 9007199254740991 {
		return false
	}
	// Directed rounding detects values just outside the safe range even when
	// ParseFloat rounds them onto its boundary. It also detects underflow to zero.
	exact, _, err := big.ParseFloat(raw, 10, 64, big.AwayFromZero)
	if err != nil {
		return true
	}
	return value == 0 && exact.Sign() != 0 || exact.Abs(exact).Cmp(big.NewFloat(9007199254740991)) > 0
}

func (p *documentParser) invalid(node *yaml.Node, pointer, code, message string) *Diagnostic {
	result := diagnostic(p.path, pointer, code, message, node)
	return &result
}

func diagnostic(path, pointer, code, message string, node *yaml.Node) Diagnostic {
	result := Diagnostic{Path: path, Document: 1, Pointer: pointer, Code: code, Message: message}
	if node != nil {
		result.Line, result.Column = node.Line, node.Column
	}
	return result
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
