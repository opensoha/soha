package proxydeployment

import "testing"

func TestRetiringRange(t *testing.T) {
	for _, tt := range []struct {
		name                                         string
		target, actual, desired, maximum, first, end int32
		valid                                        bool
	}{
		{"2 to 1", 1, 2, 2, 3, 1, 2, true}, {"3 to 1", 1, 3, 3, 3, 1, 3, true}, {"stop", 0, 2, 2, 3, 0, 2, true}, {"old desired greater", 1, 1, 3, 3, 1, 3, true}, {"up", 3, 1, 1, 3, 1, 1, true}, {"negative target", -1, 2, 2, 3, 0, 0, false}, {"unknown actual", 1, -1, 2, 3, 0, 0, false}, {"excess observation", 1, 4, 2, 3, 0, 0, false}, {"excess desired", 1, 2, 4, 3, 0, 0, false}, {"excess target", 4, 2, 2, 3, 0, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := RetiringRange(tt.target, tt.actual, tt.desired, tt.maximum)
			if valid != tt.valid || valid && (got.First != tt.first || got.End != tt.end || got.Count() != tt.end-tt.first) {
				t.Fatalf("range=%+v valid=%v", got, valid)
			}
		})
	}
}
