package runtime

import (
	"strings"
	"testing"
)

func TestOriginalContainerSetNeverAdoptsUnknownOrReplacement(t *testing.T) {
	project, _ := NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	one := ContainerBinding{ProjectBinding: project, ServiceName: "one", ContainerID: strings.Repeat("a", 64)}
	two := ContainerBinding{ProjectBinding: project, ServiceName: "two", ContainerID: strings.Repeat("b", 64)}
	set, err := ContainerSetFromValue([]ContainerBinding{one, two}, project)
	if err != nil || !SameContainerSet(set, []ContainerBinding{two, one}) {
		t.Fatal("valid unordered container set rejected", err)
	}
	for _, raw := range []any{nil, "[]", []ContainerBinding{one, one}, []any{map[string]any{"containerId": one.ContainerID}}, make([]ContainerBinding, 257)} {
		if _, err := ContainerSetFromValue(raw, project); err == nil {
			t.Fatal("ambiguous original inventory accepted", raw)
		}
	}
	if empty, err := ContainerSetFromValue([]ContainerBinding{}, project); err != nil || len(empty) != 0 {
		t.Fatal(err)
	}
	foreign := two
	foreign.ContainerID = strings.Repeat("c", 64)
	for _, actual := range [][]ContainerBinding{{one}, {one, foreign}, {one, two, foreign}, {one, one}} {
		if SameContainerSet(set, actual) {
			t.Fatal("changed inventory accepted")
		}
	}
}
