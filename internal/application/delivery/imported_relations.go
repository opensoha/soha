package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	cr "github.com/opensoha/soha-contracts/resource"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type importRelationsReader interface {
	GetApplicationResourceRelations(context.Context, di.Principal, dr.ResourceSourceSnapshot, cr.RelationConfig) (cr.ResourceRelationsView, error)
}

func (s *Service) GetImportedTargetRelations(ctx context.Context, p di.Principal, bindingID, targetID string) (cr.ResourceRelationsView, error) {
	binding, err := s.catalog.GetApplicationEnvironment(ctx, p, bindingID)
	if err != nil {
		return cr.ResourceRelationsView{}, err
	}
	if _, err = s.applications.Get(ctx, p, binding.ApplicationID); err != nil {
		return cr.ResourceRelationsView{}, err
	}
	reader, ok := s.targets.(importRelationsReader)
	if !ok {
		return cr.ResourceRelationsView{}, fmt.Errorf("%w: resource relationships unavailable", apperrors.ErrUnsupportedOperation)
	}
	for _, target := range binding.Targets {
		if target.ID != targetID || metadataString(target.Metadata, "importSource") != "kubernetes" {
			continue
		}
		source, err := importedTargetSource(target)
		if err != nil {
			return cr.ResourceRelationsView{}, err
		}
		var config cr.RelationConfig
		if value, exists := target.Metadata["resourceRelations"]; exists {
			data, err := json.Marshal(value)
			if err != nil || len(data) > 16<<10 {
				return cr.ResourceRelationsView{}, apperrors.ErrInvalidArgument
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || cr.ValidateRelationConfig(config) != nil {
				return cr.ResourceRelationsView{}, fmt.Errorf("%w: invalid relation configuration", apperrors.ErrInvalidArgument)
			}
		}
		return reader.GetApplicationResourceRelations(ctx, p, source, config)
	}
	return cr.ResourceRelationsView{}, fmt.Errorf("%w: linked target not found", apperrors.ErrNotFound)
}
