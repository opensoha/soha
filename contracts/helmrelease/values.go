package helmrelease

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// MapImages returns a copy; mapping never changes a saved values configuration.
func MapImages(values map[string]any, mappings []sohaapi.HelmImageMapping, images map[string]string) (map[string]any, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	for _, mapping := range mappings {
		image := images[mapping.ContainerName]
		repository, digest, ok := strings.Cut(image, "@")
		if !ok || repository == "" || !helmSHA256.MatchString(digest) {
			return nil, fmt.Errorf("helm image mapping requires a verified digest for container %s", mapping.ContainerName)
		}
		value := image
		switch mapping.Value {
		case sohaapi.HelmImageMappingValueRepository:
			value = repository
		case sohaapi.HelmImageMappingValueDigest:
			value = digest
		case sohaapi.HelmImageMappingValueImage:
		default:
			return nil, errors.New("unsupported Helm image mapping value")
		}
		parts, err := pointerParts(mapping.Path)
		if err != nil {
			return nil, err
		}
		if err := setPointer(result, parts, value); err != nil {
			return nil, fmt.Errorf("helm image path %s: %w", mapping.Path, err)
		}
	}
	return result, nil
}

func pointerParts(pointer string) ([]string, error) {
	if !strings.HasPrefix(pointer, "/") || len(pointer) > 512 {
		return nil, errors.New("helm image path must be an RFC 6901 pointer of at most 512 characters")
	}
	parts := strings.Split(pointer[1:], "/")
	for index, part := range parts {
		for offset := 0; offset < len(part); offset++ {
			if part[offset] == '~' {
				if offset+1 >= len(part) || part[offset+1] != '0' && part[offset+1] != '1' {
					return nil, errors.New("invalid escape in Helm image path")
				}
				offset++
			}
		}
		parts[index] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func setPointer(node any, parts []string, value string) error {
	key := parts[0]
	switch typed := node.(type) {
	case map[string]any:
		if len(parts) == 1 {
			typed[key] = value
			return nil
		}
		child, ok := typed[key]
		if !ok {
			child = map[string]any{}
			typed[key] = child
		}
		return setPointer(child, parts[1:], value)
	case []any:
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(typed) || strconv.Itoa(index) != key {
			return errors.New("array index is missing or out of bounds")
		}
		if len(parts) == 1 {
			typed[index] = value
			return nil
		}
		return setPointer(typed[index], parts[1:], value)
	default:
		return errors.New("path traverses a scalar value")
	}
}
