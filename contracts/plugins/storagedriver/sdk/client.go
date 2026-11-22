package sdk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

// ClientOptions identifies the launched driver. LaunchSecret remains owned by the
// caller, which should erase it when Dial returns. TLSLeafSHA256 is required for tcp.
type ClientOptions struct {
	Network       string // unix or tcp
	Address       string
	Expected      storagedriver.Identity
	LaunchSecret  []byte
	TLSLeafSHA256 [sha256.Size]byte
	CallTimeout   time.Duration // Zero uses the default of five minutes.
}

// Client is an authenticated connection to one launch. Its calls can run concurrently.
type Client struct {
	http        *http.Client
	baseURL     string
	bearer      string
	callTimeout time.Duration
	lifetime    context.Context
	close       context.CancelFunc
}

var _ storagedriver.Driver = (*Client)(nil)

// Dial completes and verifies the launch handshake. ctx governs the handshake only.
// The caller owns the returned client and must close it before retiring the launch.
func Dial(ctx context.Context, cfg ClientOptions) (*Client, error) {
	noPin := cfg.TLSLeafSHA256 == [sha256.Size]byte{}
	if !validAddress(cfg.Network, cfg.Address) || (cfg.Network == "tcp" && noPin) ||
		(cfg.Network == "unix" && !noPin) || cfg.CallTimeout < 0 {
		return nil, ErrBootstrap
	}
	request, err := storagedriver.NewHandshakeRequest(cfg.LaunchSecret, cfg.Expected)
	if err != nil {
		return nil, err
	}
	lifetime, closeClient := context.WithCancel(context.Background())
	client := &Client{
		http: &http.Client{
			Transport:     clientTransport(cfg),
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		baseURL: "http://storage-driver", callTimeout: cfg.CallTimeout, lifetime: lifetime, close: closeClient,
	}
	if cfg.Network == "tcp" {
		client.baseURL = "https://storage-driver"
	}
	if client.callTimeout == 0 {
		client.callTimeout = defaultCallTimeout
	}
	var response storagedriver.HandshakeResponse
	if err := client.control(ctx, storagedriver.PathHandshake, request, &response); err != nil {
		_ = client.Close()
		return nil, err
	}
	token, err := response.Verify(cfg.LaunchSecret, request)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.bearer = "Bearer " + token
	return client, nil
}

// Close cancels active calls and closes idle transport connections.
func (c *Client) Close() error {
	c.close()
	c.http.CloseIdleConnections()
	return nil
}

func (c *Client) TestConnection(ctx context.Context, binding storagedriver.Binding) error {
	if err := validateRequest(binding.Validate()); err != nil {
		return err
	}
	return c.control(ctx, storagedriver.PathTestConnection, storagedriver.TestConnectionRequest{Binding: binding}, nil)
}

// PutObject streams body to the driver. If body implements io.ReadCloser, the
// transport may close it on return or cancellation. A blocking reader should
// honor cancellation itself or unblock when closed.
func (c *Client) PutObject(ctx context.Context, req storagedriver.PutObjectRequest,
	body io.Reader,
) (storagedriver.PutObjectResult, error) {
	if err := validateRequest(req.Validate()); err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	if body == nil {
		return storagedriver.PutObjectResult{}, rpcError(storagedriver.CodeInvalidRequest)
	}
	metadata, err := storagedriver.EncodeMetadata(req)
	if err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	ctx, done := c.callContext(ctx)
	defer done()
	if closer, ok := body.(io.ReadCloser); ok {
		upload := &uploadBody{Reader: closer, close: sync.OnceValue(closer.Close)}
		stop := context.AfterFunc(ctx, func() { _ = upload.Close() })
		defer stop()
		body = upload
	}
	headers := http.Header{
		"Content-Type":               {storagedriver.MediaTypeBinary},
		storagedriver.HeaderMetadata: {metadata},
	}
	response, err := c.call(ctx, http.MethodPost, storagedriver.PathPutObject, headers, body)
	if err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	defer response.Body.Close()
	var result storagedriver.PutObjectResult
	if err := readResponse(ctx, response, &result); err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	if result.Validate() != nil {
		return storagedriver.PutObjectResult{}, ErrProtocol
	}
	return result, nil
}

func (c *Client) OpenObject(ctx context.Context, req storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	if err := validateRequest(req.Validate()); err != nil {
		return storagedriver.OpenObjectResult{}, err
	}
	metadata, err := storagedriver.EncodeMetadata(req)
	if err != nil {
		return storagedriver.OpenObjectResult{}, err
	}
	headers := http.Header{storagedriver.HeaderMetadata: {metadata}}
	if req.IfNoneMatch != "" {
		headers.Set("If-None-Match", req.IfNoneMatch)
	}
	if req.Range != "" {
		headers.Set("Range", req.Range)
	}
	ctx, done := c.callContext(ctx)
	response, err := c.call(ctx, http.MethodGet, storagedriver.PathOpenObject, headers, nil)
	if err != nil {
		done()
		return storagedriver.OpenObjectResult{}, err
	}
	result, err := parseOpen(ctx, response, req)
	if err != nil || result.NotModified {
		_ = response.Body.Close()
		done()
		return result, err
	}
	result.Body = &openBody{ReadCloser: result.Body, ctx: ctx, done: done}
	return result, nil
}

func (c *Client) DeleteKeys(ctx context.Context, req storagedriver.DeleteKeysRequest) error {
	if err := validateRequest(req.Validate()); err != nil {
		return err
	}
	return c.control(ctx, storagedriver.PathDeleteKeys, req, nil)
}

func (c *Client) ListPrefix(ctx context.Context, req storagedriver.ListPrefixRequest) (storagedriver.ListPrefixResult, error) {
	if err := validateRequest(req.Validate()); err != nil {
		return storagedriver.ListPrefixResult{}, err
	}
	var result storagedriver.ListPrefixResult
	if err := c.control(ctx, storagedriver.PathListPrefix, req, &result); err != nil {
		return storagedriver.ListPrefixResult{}, err
	}
	if result.Validate(req) != nil {
		return storagedriver.ListPrefixResult{}, ErrProtocol
	}
	return result, nil
}

func (c *Client) callContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	stop := context.AfterFunc(c.lifetime, cancel)
	if c.lifetime.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func (c *Client) control(ctx context.Context, path string, input, output any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	if len(raw) > storagedriver.MaxControlBytes {
		return rpcError(storagedriver.CodeTooLarge)
	}
	ctx, done := c.callContext(ctx)
	defer done()
	headers := http.Header{"Content-Type": {storagedriver.MediaTypeJSON}}
	response, err := c.call(ctx, http.MethodPost, path, headers, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return readResponse(ctx, response, output)
}

func (c *Client) call(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, ErrProtocol
	}
	request.GetBody = nil
	request.Header = headers
	request.Header.Set("Cache-Control", "no-store")
	if c.bearer != "" {
		request.Header.Set("Authorization", c.bearer)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, transportError(ctx)
	}
	if len(response.Header.Values("Content-Encoding")) != 0 || response.ProtoMajor != 1 || response.ProtoMinor != 1 {
		_ = response.Body.Close()
		return nil, ErrProtocol
	}
	return response, nil
}

func readResponse(ctx context.Context, response *http.Response, target any) error {
	switch {
	case target == nil && response.StatusCode == http.StatusNoContent:
		if response.ContentLength == 0 && len(response.TransferEncoding) == 0 {
			return nil
		}
	case target != nil && response.StatusCode == http.StatusOK:
		if hasContentType(response.Header, storagedriver.MediaTypeJSON) &&
			readJSON(response.Body, storagedriver.MaxControlBytes, target) == nil {
			return nil
		}
	case response.StatusCode >= http.StatusBadRequest:
		return readRPCError(ctx, response)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrProtocol
}

func readRPCError(ctx context.Context, response *http.Response) error {
	var rpc storagedriver.RPCError
	if hasContentType(response.Header, storagedriver.MediaTypeJSON) &&
		readJSON(response.Body, storagedriver.MaxControlBytes, &rpc) == nil &&
		rpc.Code.HTTPStatus() == response.StatusCode {
		return rpc
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrProtocol
}

func parseOpen(ctx context.Context, response *http.Response, req storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	if response.StatusCode >= http.StatusBadRequest {
		return storagedriver.OpenObjectResult{}, readRPCError(ctx, response)
	}
	etags := response.Header.Values("ETag")
	if len(etags) != 1 || len(response.TransferEncoding) != 0 {
		return storagedriver.OpenObjectResult{}, ErrProtocol
	}
	result := storagedriver.OpenObjectResult{ETag: etags[0]}
	lengths := response.Header.Values("Content-Length")
	types := response.Header.Values("Content-Type")
	spans := response.Header.Values("Content-Range")
	switch response.StatusCode {
	case http.StatusNotModified:
		if len(lengths) != 0 || len(types) != 0 || len(spans) != 0 {
			return storagedriver.OpenObjectResult{}, ErrProtocol
		}
		result.NotModified = true
	case http.StatusOK, http.StatusPartialContent:
		if len(lengths) != 1 || len(types) != 1 || response.ContentLength < 0 {
			return storagedriver.OpenObjectResult{}, ErrProtocol
		}
		result.ContentLength, result.ContentType, result.Body = response.ContentLength, types[0], response.Body
		if response.StatusCode == http.StatusPartialContent {
			if len(spans) != 1 {
				return storagedriver.OpenObjectResult{}, ErrProtocol
			}
			var span storagedriver.ContentRange
			if _, err := fmt.Sscanf(spans[0], "bytes %d-%d/%d", &span.Start, &span.End, &span.Size); err != nil ||
				fmt.Sprintf("bytes %d-%d/%d", span.Start, span.End, span.Size) != spans[0] {
				return storagedriver.OpenObjectResult{}, ErrProtocol
			}
			result.ContentRange = &span
		} else if len(spans) != 0 {
			return storagedriver.OpenObjectResult{}, ErrProtocol
		}
	default:
		return storagedriver.OpenObjectResult{}, ErrProtocol
	}
	if result.Validate(req) != nil {
		return storagedriver.OpenObjectResult{}, ErrProtocol
	}
	return result, nil
}

type openBody struct {
	io.ReadCloser
	ctx  context.Context
	done func()
}

type uploadBody struct {
	io.Reader
	close func() error
}

func (body *uploadBody) Close() error { return body.close() }

func (body *openBody) Read(p []byte) (int, error) {
	n, err := body.ReadCloser.Read(p)
	if err == nil {
		return n, nil
	}
	defer body.done()
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return n, err
	}
	return n, transportError(body.ctx)
}

func (body *openBody) Close() error {
	body.done()
	if err := body.ReadCloser.Close(); err != nil {
		return ErrTransport
	}
	return nil
}

func transportError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrTransport
}
