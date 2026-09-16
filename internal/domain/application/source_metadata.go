package application

import (
	"context"
	"errors"
)

var ErrSourceFileTooLarge = errors.New("source file exceeds analysis limit")

// SourceMetadataReader is an optional provider capability. Plain Git repositories
// do not gain a server-side checkout fallback.
type SourceMetadataReader interface {
	RepositoryCloneURLs(context.Context, string) ([]string, error)
	ResolveRepositoryRef(context.Context, string, string, string) (string, error)
	ReadRepositoryFile(context.Context, string, string, string, int64) ([]byte, error)
}
