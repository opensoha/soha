package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
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
	for start, startByte := 0, 0; start < len(runes); {
		end := min(start+defaultChunkRunes, len(runes))
		rawPart := string(runes[start:end])
		trimmedLeft := strings.TrimLeftFunc(rawPart, unicode.IsSpace)
		part := strings.TrimRightFunc(trimmedLeft, unicode.IsSpace)
		if part != "" {
			leadBytes := len(rawPart) - len(trimmedLeft)
			chunks = append(chunks, domainknowledge.Chunk{
				ID: uuid.NewString(), KnowledgeBaseID: document.KnowledgeBaseID,
				DocumentID: document.ID, DocumentTitle: document.Title,
				Ordinal: len(chunks), Content: part, ContentHash: contentHash(part),
				Location:   domainknowledge.SourceLocation{URI: document.URI, StartByte: startByte + leadBytes, EndByte: startByte + leadBytes + len(part)},
				TokenCount: estimateTokens(part), ACL: document.ACL, CreatedAt: document.UpdatedAt,
			})
		}
		if end == len(runes) {
			break
		}
		startByte += len(string(runes[start : end-chunkOverlapRunes]))
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
