package copilot

import (
	"context"
	"strings"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalBrowserFetcher interface {
	Fetch(context.Context, guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error)
}

func (s *PersonalGuestService) ConfigureBrowserNetwork(fetcher personalBrowserFetcher) {
	s.browserNetwork = fetcher
}
func (s *PersonalGuestService) BrowserNetwork(ctx context.Context, identity networkidentity.Identity, id string, input guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error) {
	var result guest.BrowserNetworkResponse
	if s.browserNetwork == nil || input.Validate() != nil {
		return result, apperrors.ErrInvalidArgument
	}
	op, err := s.RuntimeOperation(ctx, identity, id)
	if err != nil {
		return result, err
	}
	if op.Kind != "browser" || op.Browser == nil {
		return result, apperrors.ErrAccessDenied
	}
	if strings.HasPrefix(op.Browser.URL, "file:") {
		return result, apperrors.ErrAccessDenied
	}
	read := input.Method == "GET" || input.Method == "HEAD" || input.Method == "OPTIONS"
	if !read && (op.Request.Lease.Mode != "restricted-write" || op.Browser.Action != "click" && op.Browser.Action != "type" && op.Browser.Action != "pointer") {
		return result, apperrors.ErrAccessDenied
	}
	// A native site mutation needs the original Gateway approval. Until native
	// browser tools carry that frozen grant, only human actions can send writes.
	if !read && op.Source != "user" {
		return result, apperrors.ErrAccessDenied
	}
	result, err = s.browserNetwork.Fetch(ctx, input)
	if err != nil {
		return guest.BrowserNetworkResponse{}, err
	}
	if _, err = s.RuntimeOperation(ctx, identity, id); err != nil {
		return guest.BrowserNetworkResponse{}, err
	}
	return result, nil
}
