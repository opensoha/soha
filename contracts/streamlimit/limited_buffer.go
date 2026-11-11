package streamlimit

import (
	"bytes"
	"io"
)

type LimitedBuffer struct {
	limit      int
	buffer     bytes.Buffer
	totalBytes int64
	truncated  bool
}

func NewLimitedBuffer(limit int) *LimitedBuffer {
	if limit <= 0 {
		limit = 1
	}
	return &LimitedBuffer{limit: limit}
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	b.totalBytes += int64(len(p))
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		chunk := p
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		if _, err := b.buffer.Write(chunk); err != nil {
			return 0, err
		}
	}
	if len(p) > remaining {
		b.truncated = true
	}
	return len(p), nil
}

func (b *LimitedBuffer) String() string {
	return b.buffer.String()
}

func (b *LimitedBuffer) TotalBytes() int64 {
	return b.totalBytes
}

func (b *LimitedBuffer) Truncated() bool {
	return b.truncated
}

func ReadString(reader io.Reader, limit int) (string, int64, bool, error) {
	buffer := NewLimitedBuffer(limit)
	if _, err := io.Copy(buffer, reader); err != nil {
		return "", 0, false, err
	}
	return buffer.String(), buffer.TotalBytes(), buffer.Truncated(), nil
}
