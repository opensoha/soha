package resource

import (
	"errors"
	"testing"

	"helm.sh/helm/v3/pkg/storage/driver"
)

func TestIsHelmReleaseNotFoundErrorUsesHelmSentinels(t *testing.T) {
	if !isHelmReleaseNotFoundError(driver.ErrReleaseNotFound) {
		t.Fatal("ErrReleaseNotFound should be recognized as not found")
	}
	if !isHelmReleaseNotFoundError(driver.ErrNoDeployedReleases) {
		t.Fatal("ErrNoDeployedReleases should be recognized as not found")
	}
	if !isHelmReleaseNotFoundError(errors.New("release foo not found")) {
		t.Fatal("legacy string match should still be recognized")
	}
	if isHelmReleaseNotFoundError(errors.New("backend connection failed")) {
		t.Fatal("unrelated error should not be recognized as not found")
	}
}
