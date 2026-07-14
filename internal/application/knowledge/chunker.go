package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const (
	defaultChunkRunes = 1200
	chunkOverlapRunes = 150
)

func chunkDocument(document domainknowledge.Document, content string) []domainknowledge.Chunk {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	runes := []rune(content)
	chunks := make([]domainknowledge.Chunk, 0, len(runes)/defaultChunkRunes+1)
	for start, ordinal := 0, 0; start < len(runes); ordinal++ {
		end := min(start+defaultChunkRunes, len(runes))
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			chunks = append(chunks, domainknowledge.Chunk{
				ID: uuid.NewString(), KnowledgeBaseID: document.KnowledgeBaseID,
				DocumentID: document.ID, DocumentTitle: document.Title,
				Ordinal: ordinal, Content: part, ContentHash: contentHash(part),
				Location:   domainknowledge.SourceLocation{URI: document.URI, StartByte: runeByteOffset(runes, start), EndByte: runeByteOffset(runes, end)},
				TokenCount: estimateTokens(part), ACL: document.ACL, CreatedAt: document.UpdatedAt,
			})
		}
		if end == len(runes) {
			break
		}
		start = end - chunkOverlapRunes
	}
	return chunks
}

func contentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func estimateTokens(value string) int {
	return max(1, (utf8.RuneCountInString(value)+3)/4)
}

func runeByteOffset(value []rune, end int) int {
	return len([]byte(string(value[:end])))
}
