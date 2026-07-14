package knowledge

import (
	"context"
	"hash/fnv"
	"math"
	"regexp"
	"strings"
	"unicode"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const localVectorDimensions = 256

var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}_-]+`)

// LocalHybridScorer provides a deterministic, dependency-free retrieval fallback.
// Production embedding and rerank adapters can replace either scorer independently.
type LocalHybridScorer struct{}

func (LocalHybridScorer) Score(query string, chunks []domainknowledge.Chunk) []float64 {
	queryTerms := uniqueTerms(query)
	scores := make([]float64, len(chunks))
	if len(queryTerms) == 0 {
		return scores
	}
	for i, chunk := range chunks {
		terms := termFrequency(chunk.Content)
		matched := 0.0
		for term := range queryTerms {
			if n := terms[term]; n > 0 {
				matched += 1 + math.Log(float64(n))
			}
		}
		scores[i] = matched / float64(len(queryTerms))
	}
	return normalizeScores(scores)
}

func (LocalHybridScorer) ScoreVector(_ context.Context, _ domainidentity.Principal, query string, chunks []domainknowledge.Chunk) ([]float64, error) {
	queryVector := hashedVector(query)
	scores := make([]float64, len(chunks))
	for i, chunk := range chunks {
		scores[i] = cosine(queryVector, hashedVector(chunk.Content))
	}
	return scores, nil
}

type LocalVectorScorer struct{ LocalHybridScorer }

func (s LocalVectorScorer) Score(ctx context.Context, principal domainidentity.Principal, query string, chunks []domainknowledge.Chunk) ([]float64, error) {
	return s.ScoreVector(ctx, principal, query, chunks)
}

func uniqueTerms(value string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, term := range tokenize(value) {
		out[term] = struct{}{}
	}
	return out
}

func termFrequency(value string) map[string]int {
	out := map[string]int{}
	for _, term := range tokenize(value) {
		out[term]++
	}
	return out
}

func tokenize(value string) []string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)
	return tokenPattern.FindAllString(value, -1)
}

func hashedVector(value string) []float64 {
	vector := make([]float64, localVectorDimensions)
	for _, term := range tokenize(value) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(term))
		sum := h.Sum64()
		index := int(sum % localVectorDimensions)
		sign := 1.0
		if sum&(1<<63) != 0 {
			sign = -1
		}
		vector[index] += sign
	}
	return vector
}

func cosine(a, b []float64) float64 {
	var dot, aa, bb float64
	for i := range a {
		dot += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return math.Max(0, dot/math.Sqrt(aa*bb))
}

func normalizeScores(scores []float64) []float64 {
	maxScore := 0.0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore == 0 {
		return scores
	}
	for i := range scores {
		scores[i] /= maxScore
	}
	return scores
}
