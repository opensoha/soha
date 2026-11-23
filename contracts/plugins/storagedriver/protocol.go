// Package storagedriver defines the local RPC contract for Soha storage plugins.
package storagedriver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

const (
	ProtocolMajor      = 1
	PathHandshake      = "/v1/storage/handshake"
	PathTestConnection = "/v1/storage/test-connection"
	PathPutObject      = "/v1/storage/put"
	PathOpenObject     = "/v1/storage/open"
	PathDeleteKeys     = "/v1/storage/delete-keys"
	PathListPrefix     = "/v1/storage/list-prefix"

	HeaderMetadata  = "X-Soha-Storage-Metadata"
	MediaTypeJSON   = "application/json"
	MediaTypeBinary = "application/octet-stream"

	MaxHeaderBytes      = 32 << 10
	MaxMetadataBytes    = 16 << 10
	MaxControlBytes     = 2 << 20
	MaxKeyBytes         = 1024
	MaxCursorBytes      = 4096
	MaxETagBytes        = 1024
	MaxIfNoneMatchBytes = 4096
	MaxContentTypeBytes = 1024
	MaxKeys             = 256
	MaxPageSize         = 256
)

type Capabilities uint32

const (
	CapabilityTestConnection Capabilities = 1 << iota
	CapabilityPutObject
	CapabilityOpenObject
	CapabilityDeleteKeys
	CapabilityListPrefix
	CapabilityConditionalOpen
	CapabilityRangeOpen

	V1Capabilities = CapabilityTestConnection | CapabilityPutObject | CapabilityOpenObject |
		CapabilityDeleteKeys | CapabilityListPrefix | CapabilityConditionalOpen | CapabilityRangeOpen
)

// Driver is the implementation contract between a storage plugin and its local RPC server.
// Calls can run concurrently. Implementations must honor cancellation and scope all provider
// operations to the supplied Binding, including any reads or temporary writes in TestConnection.
type Driver interface {
	TestConnection(ctx context.Context, binding Binding) error
	PutObject(ctx context.Context, req PutObjectRequest, body io.Reader) (PutObjectResult, error)
	OpenObject(ctx context.Context, req OpenObjectRequest) (OpenObjectResult, error)
	DeleteKeys(ctx context.Context, req DeleteKeysRequest) error
	ListPrefix(ctx context.Context, req ListPrefixRequest) (ListPrefixResult, error)
}

// Binding is supplied afresh for each call. Credentials and settings may be read only
// for that call, including the lifetime of an open stream. Never log the Binding.
type Binding struct {
	ID          string            `json:"id"`
	Container   string            `json:"container"`
	Prefix      string            `json:"prefix,omitempty"`
	Settings    json.RawMessage   `json:"settings,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"`
}

type TestConnectionRequest struct {
	Binding Binding `json:"binding"`
}

type PutObjectRequest struct {
	Binding     Binding `json:"binding"`
	Key         string  `json:"key"`
	ContentType string  `json:"contentType"`
}

type PutObjectResult struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	ETag   string `json:"etag"`
}

type OpenObjectRequest struct {
	Binding     Binding `json:"binding"`
	Key         string  `json:"key"`
	IfNoneMatch string  `json:"-"` // HTTP If-None-Match request header.
	Range       string  `json:"-"` // HTTP Range request header.
}

// OpenObjectResult maps to HTTP headers and, except on 304, an object stream.
// ContentLength is the number of bytes returned, including for a partial response.
// The receiver owns Body and must close it, including on early abandonment.
type OpenObjectResult struct {
	ETag          string
	ContentType   string
	ContentLength int64
	ContentRange  *ContentRange
	NotModified   bool
	Body          io.ReadCloser
}

// ContentRange is an inclusive interval in a representation of Size bytes.
type ContentRange struct {
	Start int64
	End   int64
	Size  int64
}

type DeleteKeysRequest struct {
	Binding Binding  `json:"binding"`
	Keys    []string `json:"keys"`
}

type ListPrefixRequest struct {
	Binding Binding `json:"binding"`
	Prefix  string  `json:"prefix"`
	Cursor  string  `json:"cursor,omitempty"`
	Limit   int     `json:"limit"`
}

type ListPrefixResult struct {
	Keys       []string `json:"keys"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type ErrorCode string

const (
	CodeInvalidRequest      ErrorCode = "invalid_request"
	CodeInvalidCursor       ErrorCode = "invalid_cursor"
	CodeUnauthenticated     ErrorCode = "unauthenticated"
	CodeAccessDenied        ErrorCode = "access_denied"
	CodeNotFound            ErrorCode = "not_found"
	CodeHandshakeMismatch   ErrorCode = "handshake_mismatch"
	CodeTooLarge            ErrorCode = "too_large"
	CodeRangeNotSatisfiable ErrorCode = "range_not_satisfiable"
	CodeBusy                ErrorCode = "busy"
	CodeInternal            ErrorCode = "internal"
	CodeUnavailable         ErrorCode = "unavailable"
	CodeDeadlineExceeded    ErrorCode = "deadline_exceeded"
)

// RPCError is the safe error envelope. It never carries provider error text.
type RPCError struct {
	Code ErrorCode `json:"code"`
}

func (e RPCError) Error() string { return string(e.Code) }

// HTTPStatus returns the wire status for a known code and zero for an unknown code.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeInvalidRequest, CodeInvalidCursor:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodeAccessDenied:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeHandshakeMismatch:
		return http.StatusConflict
	case CodeTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeRangeNotSatisfiable:
		return http.StatusRequestedRangeNotSatisfiable
	case CodeBusy:
		return http.StatusTooManyRequests
	case CodeInternal:
		return http.StatusInternalServerError
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return 0
	}
}
