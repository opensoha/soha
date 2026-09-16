package catalog

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func BuildTemplateReference(config map[string]any) (string, int64, error) {
	id, _ := config["buildTemplateId"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", 0, fmt.Errorf("%w: buildTemplateId is required", apperrors.ErrInvalidArgument)
	}
	value, present := config["buildTemplateVersion"]
	if !present {
		return id, 0, nil
	}
	data, err := json.Marshal(value)
	var version int64
	if err != nil || json.Unmarshal(data, &version) != nil || version < 1 {
		return "", 0, fmt.Errorf("%w: buildTemplateVersion must be a positive integer", apperrors.ErrInvalidArgument)
	}
	return id, version, nil
}
