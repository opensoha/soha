package resourcebackend

import (
	"io"
	"strings"
	"testing"
)

func TestHelmChartDownloadLimitIncludesExactBoundary(t *testing.T) {
	for _, size := range []int{3, 4, 5} {
		body := &helmChartBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("a", size))), remaining: 4}
		content, err := io.ReadAll(body)
		if (err != nil) != (size > 4) || len(content) != min(size, 4) {
			t.Fatalf("size %d: read %d bytes, err=%v", size, len(content), err)
		}
	}
}
