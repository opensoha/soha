//go:build !linux

package personalguest

import (
	"context"
	"errors"
)

func InitializeData(context.Context, string) (DataResult, error) {
	return DataResult{}, errors.New("personal data initialization requires Linux")
}

func ReconcileData(context.Context, string) (DataResult, error) {
	return DataResult{}, errors.New("personal data reconciliation requires Linux")
}
