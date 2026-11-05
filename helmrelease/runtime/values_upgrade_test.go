package runtime

import (
	"context"
	"errors"
	"io"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/release"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

func TestGuardValuesUpgradeRevision(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expected      int
		change        bool
		collision     bool
		wantRevisions int
		wantWrites    int
	}{
		{name: "stale before upgrade", expected: 1, wantRevisions: 2},
		{name: "external pending revision after editor read", expected: 2, change: true, wantRevisions: 3},
		{name: "same revision storage collision", expected: 2, collision: true, wantRevisions: 3},
		{name: "current revision", expected: 2, wantRevisions: 3, wantWrites: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart := &chartv2.Chart{Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "app", Version: "1.0.0"}}
			first := &releasev1.Release{Name: "app", Namespace: "test", Version: 1, Chart: chart, Info: &releasev1.Info{Status: releasecommon.StatusSuperseded}}
			current := &releasev1.Release{Name: "app", Namespace: "test", Version: 2, Chart: chart, Info: &releasev1.Info{Status: releasecommon.StatusDeployed}}
			winner := &releasev1.Release{Name: "app", Namespace: "test", Version: 3, Chart: chart, Info: &releasev1.Info{Status: releasecommon.StatusPendingUpgrade}, Config: map[string]any{"winner": true}}
			history := storage.Init(driver.NewMemory())
			for _, item := range []*releasev1.Release{first, current} {
				if err := history.Create(item); err != nil {
					t.Fatal(err)
				}
			}
			kubeClient := &valuesUpgradeKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}, history: history}
			racingDriver := valuesUpgradeRaceDriver{Driver: history.Driver}
			if tc.collision {
				racingDriver.winner = winner
			}
			cfg := &action.Configuration{Releases: storage.Init(racingDriver), KubeClient: kubeClient, Capabilities: common.DefaultCapabilities}
			upgrade := action.NewUpgrade(cfg)
			upgrade.Namespace = current.Namespace
			upgrade.MaxHistory = 1
			err := GuardValuesUpgradeRevision(cfg, upgrade, current, tc.expected)
			if tc.change {
				if err := history.Create(winner); err != nil {
					t.Fatal(err)
				}
			}
			if err == nil {
				_, err = upgrade.RunWithContext(context.Background(), current.Name, chart, map[string]any{"edited": true})
			}
			if tc.wantWrites == 0 && !errors.Is(err, ErrConflict) || tc.wantWrites > 0 && err != nil {
				t.Fatalf("upgrade error = %v", err)
			}
			if kubeClient.writes != tc.wantWrites {
				t.Fatalf("Kubernetes updates = %d, want %d", kubeClient.writes, tc.wantWrites)
			}
			if kubeClient.writes > 0 && !kubeClient.reservedBeforeUpdate {
				t.Fatal("Kubernetes updated before revision 3 was reserved")
			}
			items, err := history.History(current.Name)
			if err != nil || len(items) != tc.wantRevisions {
				t.Fatalf("history contains %d revisions, want %d, err=%v", len(items), tc.wantRevisions, err)
			}
			if tc.change || tc.collision {
				latest, err := history.Last(current.Name)
				if err != nil {
					t.Fatal(err)
				}
				if latest.(*releasev1.Release).Config["winner"] != true {
					t.Fatal("concurrent writer's Values were overwritten")
				}
			}
		})
	}
}

type valuesUpgradeRaceDriver struct {
	driver.Driver
	winner *releasev1.Release
}

func (d valuesUpgradeRaceDriver) Create(key string, item release.Releaser) error {
	if d.winner != nil {
		if err := d.Driver.Create(key, d.winner); err != nil {
			return err
		}
	}
	return d.Driver.Create(key, item)
}

type valuesUpgradeKubeClient struct {
	kubefake.PrintingKubeClient
	history              *storage.Storage
	writes               int
	reservedBeforeUpdate bool
}

func (c *valuesUpgradeKubeClient) Update(original, target kube.ResourceList, opts ...kube.ClientUpdateOption) (*kube.Result, error) {
	c.writes++
	latest, err := c.history.Last("app")
	if err == nil {
		c.reservedBeforeUpdate = latest.(*releasev1.Release).Version == 3 && latest.(*releasev1.Release).Info.Status == releasecommon.StatusPendingUpgrade
	}
	return c.PrintingKubeClient.Update(original, target, opts...)
}
