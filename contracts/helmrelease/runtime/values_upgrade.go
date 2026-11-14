package runtime

import (
	"errors"
	"fmt"

	"github.com/opensoha/soha/contracts/helmrelease"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/release"
	helmreleasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// GuardValuesUpgradeRevision protects a single Values upgrade on a request-local Helm configuration.
func GuardValuesUpgradeRevision(cfg *action.Configuration, upgrade *action.Upgrade, current *helmreleasev1.Release, expected int) error {
	if expected == 0 {
		return nil
	}
	if !helmrelease.ValuesExpectedRevision(expected).Valid() || current.Version != expected ||
		(current.Info != nil && current.Info.Status.IsPending()) {
		return helmValuesRevisionConflict()
	}
	// Storage.Create otherwise prunes history before it attempts to reserve the revision.
	upgrade.MaxHistory = 0
	cfg.Releases.Driver = valuesRevisionDriver{Driver: cfg.Releases.Driver, name: current.Name, expected: expected}
	return nil
}

type valuesRevisionDriver struct {
	driver.Driver
	name     string
	expected int
}

func (d valuesRevisionDriver) Query(labels map[string]string) ([]release.Releaser, error) {
	items, err := d.Driver.Query(labels)
	// Helm's preparation reads history again and can encounter a newer pending release.
	if len(labels) != 2 || labels["name"] != d.name || labels["owner"] != "helm" {
		return items, err
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, helmValuesRevisionConflict()
	}
	if err != nil {
		return nil, err
	}
	latest := 0
	for _, item := range items {
		revision, err := release.NewAccessor(item)
		if err != nil {
			return nil, err
		}
		latest = max(latest, revision.Version())
	}
	if latest != d.expected {
		return nil, helmValuesRevisionConflict()
	}
	return items, nil
}

func (d valuesRevisionDriver) Create(key string, item release.Releaser) error {
	revision, err := release.NewAccessor(item)
	if err != nil {
		return err
	}
	if revision.Name() != d.name || revision.Version() != d.expected+1 {
		return helmValuesRevisionConflict()
	}
	err = d.Driver.Create(key, item)
	if errors.Is(err, driver.ErrReleaseExists) {
		return helmValuesRevisionConflict()
	}
	return err
}

func helmValuesRevisionConflict() error {
	return fmt.Errorf("%w: Helm release revision changed or is pending; refresh its values before saving", ErrConflict)
}
