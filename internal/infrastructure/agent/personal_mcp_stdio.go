package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/websocket"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (c *Client) OpenPersonalMCPStdio(ctx context.Context, request contractenv.PersonalMCPStdioRequest) (*websocket.Conn, error) {
	if c.token == "" || request.Validate() != nil {
		return nil, errors.New("invalid personal MCP binding")
	}
	raw, err := json.Marshal(request)
	if err != nil || base64.RawURLEncoding.EncodedLen(len(raw)) > 4096 {
		return nil, errors.New("invalid personal MCP request")
	}
	endpoint, err := c.websocketEndpoint("/api/v1/runtime/personal-environments/mcp-stdio")
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+c.token)
	headers.Set(contractenv.PersonalMCPStdioHeader, base64.RawURLEncoding.EncodeToString(raw))
	conn, resp, err := c.wsDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if resp != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			switch resp.StatusCode {
			case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests:
				return nil, apperrors.ErrConflict
			}
		}
		return nil, errors.New("personal MCP Agent stream unavailable")
	}
	return conn, nil
}
