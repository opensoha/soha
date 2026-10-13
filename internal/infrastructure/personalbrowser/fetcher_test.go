package personalbrowser

import (
	"context"
	"testing"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func TestBrowserNetworkPrivateTargets(t *testing.T) {
	f := NewFetcher()
	for _, target := range []string{"https://127.0.0.1/", "https://10.0.0.12/", "https://100.64.0.1/", "https://169.254.169.254/", "https://[::1]/", "https://[::ffff:127.0.0.1]/", "https://198.18.0.1/", "https://localhost/", "http://example.com/", "https://example.com:8006/", "https://user:secret@example.com/"} {
		t.Run(target, func(t *testing.T) {
			if _, err := f.Fetch(context.Background(), guest.BrowserNetworkRequest{URL: target, Method: "GET"}); err == nil {
				t.Fatal("private or invalid target fetched")
			}
		})
	}
}
