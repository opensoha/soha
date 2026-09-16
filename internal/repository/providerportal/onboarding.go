package providerportal

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	providerrepo "github.com/opensoha/soha/internal/repository/identityprovider"
	"gorm.io/gorm"
)

func (r *Repository) WithinOnboardingTransaction(ctx context.Context, create func(domainportal.Repository, domainprovider.Repository) error) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return create(New(tx), providerrepo.New(tx))
	})
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" {
		return fmt.Errorf("%w: application slug or client ID already exists; continue configuring the existing resource", apperrors.ErrConflict)
	}
	return err
}
