package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	cr "github.com/opensoha/soha-contracts/resource"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateRelationTargets(targets []dc.ReleaseTargetInput) error {
	for _, target := range targets {
		value, exists := target.Metadata["resourceRelations"]
		if !exists {
			continue
		}
		invalid := fmt.Errorf("%w: relation rules require a valid imported target and matching root UID", apperrors.ErrInvalidArgument)
		if target.Metadata["importSource"] != "kubernetes" {
			return invalid
		}
		data, err := json.Marshal(value)
		if err != nil || len(data) > 16<<10 {
			return invalid
		}
		var config cr.RelationConfig
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || cr.ValidateRelationConfig(config) != nil {
			return invalid
		}
		var source struct {
			Resource struct {
				UID string `json:"uid"`
			} `json:"resource"`
		}
		if json.Unmarshal([]byte(metadataString(target.Metadata, "resourceSource")), &source) != nil || source.Resource.UID != config.RootUID {
			return invalid
		}
	}
	return nil
}

// A binding update replaces targets as a set. Always fence this replacement
// when either the stored or submitted binding contains relation rules.
func validateRelationRuleRevision(input dc.ApplicationEnvironmentInput, current dc.ApplicationEnvironment) error {
	if input.ExpectedUpdatedAt != nil {
		return nil
	}
	for _, target := range current.Targets {
		if _, present := target.Metadata["resourceRelations"]; present {
			return fmt.Errorf("%w: updating a binding with relation rules requires expectedUpdatedAt", apperrors.ErrInvalidArgument)
		}
	}
	for _, target := range input.Targets {
		if _, present := target.Metadata["resourceRelations"]; present {
			return fmt.Errorf("%w: updating relation rules requires expectedUpdatedAt", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}
