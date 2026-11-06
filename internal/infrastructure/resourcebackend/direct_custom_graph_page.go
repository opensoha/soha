package resourcebackend

import (
	"context"
	"encoding/json"

	cr "github.com/opensoha/soha-contracts/resource"
	shared "github.com/opensoha/soha-contracts/resource/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func (d *Direct) ReadCustomResourceGraphPage(ctx context.Context, clusterID string, definition cr.CRDResourceDefinition, namespace string) (cr.CustomResourceGraphPage, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return cr.CustomResourceGraphPage{}, err
	}
	return shared.ReadCustomResourceGraphPage(ctx, bundle.RESTConfig, definition, namespace)
}

func (d *Direct) ReadCustomResourceGraphObject(ctx context.Context, clusterID string, request cr.CustomResourceGraphObjectRequest) (json.RawMessage, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	item, err := shared.ReadCustomResourceGraphObject(ctx, bundle.RESTConfig, request)
	switch {
	case err == nil:
		return item, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case apierrors.IsNotFound(err):
		return nil, apperrors.ErrNotFound
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return nil, apperrors.ErrAccessDenied
	case apierrors.IsBadRequest(err) || apierrors.IsInvalid(err):
		return nil, apperrors.ErrInvalidArgument
	default:
		return nil, apperrors.ErrClusterUnready
	}
}
