package networkruntime

import (
	"context"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func (r *Repository) RequiresManagedVPN(ctx context.Context, subjectID, deviceID string) (bool, error) {
	return requiresManagedVPN(r.db.WithContext(ctx), subjectID, deviceID)
}

func requiresManagedVPN(tx *gorm.DB, subjectID, deviceID string) (bool, error) {
	repo := networkrepo.New(tx)
	subject, err := repo.GetSubject(tx.Statement.Context, subjectID)
	if err != nil {
		return false, err
	}
	device, err := repo.GetDevice(tx.Statement.Context, deviceID)
	if err != nil {
		return false, err
	}
	filter := domain.VPNDocumentFilter{Limit: 200}
	for {
		profiles, err := repo.ListVPNProfiles(tx.Statement.Context, filter)
		if err != nil {
			return false, err
		}
		for _, profile := range profiles {
			if c := profile.PublishedConfiguration; c != nil && c.Enabled && c.Assignments.Matches(subject, device) {
				return true, nil
			}
		}
		if len(profiles) < 200 {
			return false, nil
		}
		filter.AfterID = profiles[len(profiles)-1].ID
	}
}
