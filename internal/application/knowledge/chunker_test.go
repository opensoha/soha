package knowledge

import (
	"strings"
	"testing"
	"unicode/utf8"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestChunkDocumentPreservesUnicodeByteLocations(t *testing.T) {
	content := strings.Repeat("a中🙂", 1100)
	runes := []rune(content)
	chunks := chunkDocument(domainknowledge.Document{ID: "doc", URI: "kb://doc"}, content)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
	for index, chunk := range chunks {
		start := index * (defaultChunkRunes - chunkOverlapRunes)
		end := min(start+defaultChunkRunes, len(runes))
		wantStart, wantEnd := len(string(runes[:start])), len(string(runes[:end]))
		if chunk.Location.StartByte != wantStart || chunk.Location.EndByte != wantEnd || chunk.Location.URI != "kb://doc" {
			t.Errorf("chunk %d location = %#v, want bytes %d:%d", index, chunk.Location, wantStart, wantEnd)
		}
		if chunk.Content != content[wantStart:wantEnd] || !utf8.ValidString(chunk.Content) {
			t.Errorf("chunk %d content does not match its byte location", index)
		}
	}
}

func BenchmarkChunkDocument(b *testing.B) {
	for _, size := range []struct {
		name  string
		runes int
	}{{"100k", 100_000}, {"1m", 1_000_000}} {
		b.Run(size.name, func(b *testing.B) {
			content := strings.Repeat("中", size.runes)
			b.ReportAllocs()
			b.SetBytes(int64(len(content)))
			for b.Loop() {
				chunkDocument(domainknowledge.Document{ID: "doc"}, content)
			}
		})
	}
}
