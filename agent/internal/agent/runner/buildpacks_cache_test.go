package runner

import (
	"strings"
	"testing"
)

func TestBuildpacksCacheCapacityAndOwnership(t *testing.T) {
	cache := func(name string, size, refs int64) buildpacksCacheVolume {
		return buildpacksCacheVolume{Name: name, UsageData: &struct{ Size, RefCount int64 }{size, refs}}
	}
	first, second := "soha-cnb-"+strings.Repeat("a", 64)+"-build", "soha-cnb-"+strings.Repeat("b", 64)+"-launch"
	volumes := []buildpacksCacheVolume{cache(first, 40, 0), cache(second, 60, 0), cache("business-volume", 1000, 1)}
	if names, total, err := buildpacksCacheEvictions(volumes, 100); err != nil || len(names) != 0 || total != 100 {
		t.Fatalf("cache at limit: %v %d %v", names, total, err)
	}
	names, total, err := buildpacksCacheEvictions(volumes, 99)
	if err != nil || len(names) != 2 || names[0] != first || names[1] != second || total != 100 {
		t.Fatalf("owned eviction: %v %d %v", names, total, err)
	}
	for _, volume := range []buildpacksCacheVolume{{Name: first}, cache(first, -1, 0), cache(first, 1, 1)} {
		if _, _, err := buildpacksCacheEvictions([]buildpacksCacheVolume{volume}, 1); err == nil {
			t.Fatal("unknown or active cache was accepted")
		}
	}
}
