package knowledge

import "errors"

var (
	ErrAccessDenied       = errors.New("knowledge access denied")
	ErrBaseNotFound       = errors.New("knowledge base not found")
	ErrSourceNotFound     = errors.New("knowledge source not found")
	ErrInvalidInput       = errors.New("invalid knowledge input")
	ErrSourceUnavailable  = errors.New("knowledge source unavailable")
	ErrRetrievalExhausted = errors.New("knowledge retrieval candidate limit exceeded")
	ErrIngestionNotFound  = errors.New("knowledge ingestion job not found")
	ErrIngestionConflict  = errors.New("knowledge ingestion job conflict")
)
