package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	cr "github.com/opensoha/soha/contracts/resource"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateRelationTargets(targets []dc.ReleaseTargetInput) error {
	for _, target := range targets {
		if target.Metadata["importSource"] == "kubernetes" && target.WorkloadKind == "CustomResource" && target.ExecutorKind == "manifest_ssa" {
			if target.Metadata["ownershipMode"] != "managed" || target.ConfigRef == "" {
				return fmt.Errorf("%w: root CR publication requires explicit managed ownership and Manifest binding", apperrors.ErrInvalidArgument)
			}
		}
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
	if err := validateImportedRootIdentity(input, current); err != nil {
		return err
	}
	if input.ExpectedUpdatedAt != nil {
		return nil
	}
	for _, target := range current.Targets {
		if _, present := target.Metadata["resourceRelations"]; present || target.Metadata["importSource"] == "kubernetes" && target.WorkloadKind == "CustomResource" {
			return fmt.Errorf("%w: updating a binding with relation rules requires expectedUpdatedAt", apperrors.ErrInvalidArgument)
		}
	}
	for _, target := range input.Targets {
		if _, present := target.Metadata["resourceRelations"]; present || target.Metadata["importSource"] == "kubernetes" && target.WorkloadKind == "CustomResource" {
			return fmt.Errorf("%w: updating relation rules requires expectedUpdatedAt", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func validateImportedRootIdentity(input dc.ApplicationEnvironmentInput, current dc.ApplicationEnvironment) error {
	for _, old := range current.Targets {
		if old.WorkloadKind != "CustomResource" || old.Metadata["importSource"] != "kubernetes" {
			continue
		}
		for _, next := range input.Targets {
			if next.ID != old.ID {
				continue
			}
			if next.ClusterID != old.ClusterID || next.Namespace != old.Namespace || next.WorkloadKind != old.WorkloadKind || next.WorkloadName != old.WorkloadName || next.Metadata["importSource"] != "kubernetes" || next.Metadata["resourceSource"] != old.Metadata["resourceSource"] {
				return fmt.Errorf("%w: imported root identity is immutable; unlink and explicitly import a replacement", apperrors.ErrConflict)
			}
		}
	}
	return nil
}
