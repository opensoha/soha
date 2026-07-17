package access

import "testing"

func TestMatchesNamespaceSelector(t *testing.T) {
	labels := map[string]string{"tenant": "retail", "managed": "true"}
	tests := []struct {
		selector string
		want     bool
	}{
		{selector: "tenant=retail", want: true},
		{selector: "tenant==retail,managed", want: true},
		{selector: "tenant!=other,!restricted", want: true},
		{selector: "tenant=other", want: false},
		{selector: "tenant=retail,", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			if got := MatchesNamespaceSelector(tt.selector, labels); got != tt.want {
				t.Fatalf("MatchesNamespaceSelector(%q) = %v, want %v", tt.selector, got, tt.want)
			}
		})
	}
}

func TestValidNamespaceSelectorRejectsAmbiguousRequirements(t *testing.T) {
	for _, selector := range []string{"tenant=", "=retail", "tenant=retail,", "!"} {
		if ValidNamespaceSelector(selector) {
			t.Fatalf("ValidNamespaceSelector(%q) = true, want false", selector)
		}
	}
}
