package knowledge

import (
	"math"
	"testing"
)

func TestEffectiveRetrievalPolicy(t *testing.T) {
	for _, test := range []struct {
		name  string
		input RetrievalPolicy
		want  RetrievalPolicy
	}{
		{name: "legacy zero", want: RetrievalPolicy{DefaultTopK: 5, MaxTopK: 20, LexicalWeight: .45, VectorWeight: .55}},
		{name: "normalize configured weights", input: RetrievalPolicy{DefaultTopK: 3, MaxTopK: 8, LexicalWeight: .2, VectorWeight: .2, MinScore: .7}, want: RetrievalPolicy{DefaultTopK: 3, MaxTopK: 8, LexicalWeight: .5, VectorWeight: .5, MinScore: .7}},
		{name: "bounded legacy values", input: RetrievalPolicy{DefaultTopK: 100, MaxTopK: 99, LexicalWeight: -1, VectorWeight: 2, MinScore: 3}, want: RetrievalPolicy{DefaultTopK: 50, MaxTopK: 50, VectorWeight: 1, MinScore: 1}},
		{name: "default capped by max", input: RetrievalPolicy{MaxTopK: 2}, want: RetrievalPolicy{DefaultTopK: 2, MaxTopK: 2, LexicalWeight: .45, VectorWeight: .55}},
		{name: "non finite legacy values", input: RetrievalPolicy{LexicalWeight: math.NaN(), VectorWeight: math.Inf(1), MinScore: math.NaN()}, want: RetrievalPolicy{DefaultTopK: 5, MaxTopK: 20, LexicalWeight: .45, VectorWeight: .55}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := EffectiveRetrievalPolicy(test.input); got != test.want {
				t.Fatalf("effective policy = %+v, want %+v", got, test.want)
			}
		})
	}
}
