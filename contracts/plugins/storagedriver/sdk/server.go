package sdk

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

// ServerOptions comes from the driver build, separately from the launch bootstrap.
type ServerOptions struct {
	PluginID    string
	Version     string
	CallTimeout time.Duration // Zero uses the default of five minutes.
}

// Serve reads one bootstrap object from private input until EOF and serves driver.
// Cancelling ctx stops the server and cancels active calls. The author normally
// passes os.Stdin and a context tied to process termination.
func Serve(ctx context.Context, input io.Reader, options ServerOptions, driver storagedriver.Driver) error {
	identity := storagedriver.Identity{
		PluginID: options.PluginID, Version: options.Version,
		ProtocolMajor: storagedriver.ProtocolMajor, Capabilities: storagedriver.V1Capabilities,
	}
	if err := identity.Validate(); err != nil {
		return err
	}
	if driver == nil || options.CallTimeout < 0 {
		return ErrBootstrap
	}
	cfg, err := readBootstrap(ctx, input)
	if err != nil {
		return err
	}
	defer cfg.erase()
	listener, err := listen(cfg)
	if err != nil {
		return err
	}
	if cfg.TLS != nil {
		clear(cfg.TLS.PrivateKeyDER)
	}
	defer func() { _ = listener.Close() }()
	return serve(ctx, listener, identity, cfg.LaunchSecret, options.CallTimeout, driver)
}

func serve(ctx context.Context, listener net.Listener, identity storagedriver.Identity, secret []byte,
	callTimeout time.Duration, driver storagedriver.Driver,
) error {
	if callTimeout == 0 {
		callTimeout = defaultCallTimeout
	}
	handler := &serverHandler{identity: identity, launchSecret: secret, driver: driver, callTimeout: callTimeout}
	defer handler.clear()
	server := &http.Server{
		Handler:           handler,
		Protocols:         http1(),
		MaxHeaderBytes:    storagedriver.MaxHeaderBytes,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0), // Provider panics may carry credentials.
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	defer func() { _ = server.Close() }()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return ErrTransport
}

type serverHandler struct {
	identity    storagedriver.Identity
	driver      storagedriver.Driver
	callTimeout time.Duration

	mu           sync.Mutex
	launchSecret []byte
	bearer       string
}

func (h *serverHandler) clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.launchSecret)
	h.launchSecret = nil
	h.bearer = ""
}

func (h *serverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != storagedriver.PathHandshake && !h.authenticated(r.Header) {
		writeError(w, rpcError(storagedriver.CodeUnauthenticated), storagedriver.CodeUnauthenticated)
		return
	}
	if r.ProtoMajor != 1 || r.ProtoMinor != 1 || r.RequestURI != r.URL.Path ||
		!headerBudget(r) || len(r.Header.Values("Content-Encoding")) != 0 {
		writeError(w, rpcError(storagedriver.CodeInvalidRequest), storagedriver.CodeInvalidRequest)
		return
	}
	var operation func(http.ResponseWriter, *http.Request) error
	method := http.MethodPost
	switch r.URL.Path {
	case storagedriver.PathHandshake:
		operation = h.handshake
	case storagedriver.PathTestConnection:
		operation = h.testConnection
	case storagedriver.PathPutObject:
		operation = h.putObject
	case storagedriver.PathOpenObject:
		method, operation = http.MethodGet, h.openObject
	case storagedriver.PathDeleteKeys:
		operation = h.deleteKeys
	case storagedriver.PathListPrefix:
		operation = h.listPrefix
	}
	if operation == nil || r.Method != method {
		writeError(w, rpcError(storagedriver.CodeInvalidRequest), storagedriver.CodeInvalidRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.callTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	control := http.NewResponseController(w)
	if control.SetReadDeadline(deadline) != nil || control.SetWriteDeadline(deadline) != nil {
		writeError(w, ErrTransport, storagedriver.CodeInternal)
		return
	}
	if err := operation(w, r.WithContext(ctx)); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		writeError(w, err, storagedriver.CodeInternal)
	}
}

func (h *serverHandler) authenticated(headers http.Header) bool {
	supplied := headers.Values("Authorization")
	if len(supplied) != 1 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bearer != "" && subtle.ConstantTimeCompare([]byte(supplied[0]), []byte(h.bearer)) == 1
}

func (h *serverHandler) handshake(w http.ResponseWriter, r *http.Request) error {
	if len(r.Header.Values("Authorization")) != 0 {
		return rpcError(storagedriver.CodeUnauthenticated)
	}
	var request storagedriver.HandshakeRequest
	if err := readRequest(r, &request); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.bearer != "" || len(h.launchSecret) == 0 {
		return rpcError(storagedriver.CodeUnauthenticated)
	}
	response, token, err := request.Accept(h.launchSecret, h.identity)
	if err != nil {
		return err
	}
	if r.Context().Err() != nil {
		return r.Context().Err()
	}
	h.bearer = "Bearer " + token
	clear(h.launchSecret)
	h.launchSecret = nil
	writeJSON(w, response)
	return nil
}

func (h *serverHandler) testConnection(w http.ResponseWriter, r *http.Request) error {
	var request storagedriver.TestConnectionRequest
	if err := readRequest(r, &request); err != nil {
		return err
	}
	if err := validateRequest(request.Binding.Validate()); err != nil {
		return err
	}
	if err := h.driver.TestConnection(r.Context(), request.Binding); err != nil {
		return err
	}
	if r.Context().Err() != nil {
		return r.Context().Err()
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *serverHandler) putObject(w http.ResponseWriter, r *http.Request) error {
	var request storagedriver.PutObjectRequest
	if !hasContentType(r.Header, storagedriver.MediaTypeBinary) {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	if err := readMetadata(r.Header, &request); err != nil {
		return err
	}
	if err := validateRequest(request.Validate()); err != nil {
		return err
	}
	body := &uploadReader{body: r.Body, hash: sha256.New()}
	result, err := h.driver.PutObject(r.Context(), request, body)
	if err != nil {
		return err
	}
	if r.Context().Err() != nil {
		return r.Context().Err()
	}
	if result.Validate() != nil || !body.complete() || result.Size != body.size ||
		result.SHA256 != hex.EncodeToString(body.hash.Sum(nil)) {
		return rpcError(storagedriver.CodeInternal)
	}
	writeJSON(w, result)
	return nil
}

func (h *serverHandler) openObject(w http.ResponseWriter, r *http.Request) error {
	var request storagedriver.OpenObjectRequest
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	if err := readMetadata(r.Header, &request); err != nil {
		return err
	}
	for field, value := range map[string]*string{"If-None-Match": &request.IfNoneMatch, "Range": &request.Range} {
		values := r.Header.Values(field)
		if len(values) > 1 || (len(values) == 1 && values[0] == "") {
			return rpcError(storagedriver.CodeInvalidRequest)
		}
		if len(values) == 1 {
			*value = values[0]
		}
	}
	if err := validateRequest(request.Validate()); err != nil {
		return err
	}
	result, err := h.driver.OpenObject(r.Context(), request)
	if result.Body != nil {
		closeBody := sync.OnceFunc(func() {
			defer func() { _ = recover() }() // Cancellation runs outside net/http's panic recovery.
			_ = result.Body.Close()
		})
		stop := context.AfterFunc(r.Context(), closeBody)
		defer func() {
			stop()
			closeBody()
		}()
	}
	if err != nil {
		return err
	}
	if r.Context().Err() != nil {
		return r.Context().Err()
	}
	if err := result.Validate(request); err != nil {
		return rpcError(storagedriver.CodeInternal)
	}
	w.Header().Set("ETag", result.ETag)
	if result.NotModified {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(result.ContentLength, 10))
	status := http.StatusOK
	if result.ContentRange != nil {
		status = http.StatusPartialContent
		span := result.ContentRange
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", span.Start, span.End, span.Size))
	}
	w.WriteHeader(status)
	if http.NewResponseController(w).Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	if _, err := io.CopyN(w, result.Body, result.ContentLength); err != nil {
		panic(http.ErrAbortHandler) // The success headers are already on the wire.
	}
	return nil
}

func (h *serverHandler) deleteKeys(w http.ResponseWriter, r *http.Request) error {
	var request storagedriver.DeleteKeysRequest
	if err := readRequest(r, &request); err != nil {
		return err
	}
	if err := validateRequest(request.Validate()); err != nil {
		return err
	}
	if err := h.driver.DeleteKeys(r.Context(), request); err != nil {
		return err
	}
	if r.Context().Err() != nil {
		return r.Context().Err()
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *serverHandler) listPrefix(w http.ResponseWriter, r *http.Request) error {
	var request storagedriver.ListPrefixRequest
	if err := readRequest(r, &request); err != nil {
		return err
	}
	if err := validateRequest(request.Validate()); err != nil {
		return err
	}
	result, err := h.driver.ListPrefix(r.Context(), request)
	if err != nil {
		return err
	}
	if r.Context().Err() != nil {
		return r.Context().Err()
	}
	if err := result.Validate(request); err != nil {
		return rpcError(storagedriver.CodeInternal)
	}
	writeJSON(w, result)
	return nil
}

func readRequest(r *http.Request, target any) error {
	if !hasContentType(r.Header, storagedriver.MediaTypeJSON) || len(r.Header.Values(storagedriver.HeaderMetadata)) != 0 {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	if r.ContentLength > storagedriver.MaxControlBytes {
		return rpcError(storagedriver.CodeTooLarge)
	}
	return readJSON(r.Body, storagedriver.MaxControlBytes, target)
}

func readMetadata(header http.Header, target any) error {
	values := header.Values(storagedriver.HeaderMetadata)
	if len(values) != 1 {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	return storagedriver.DecodeMetadata(values[0], target)
}

func hasContentType(header http.Header, want string) bool {
	types := header.Values("Content-Type")
	return len(types) == 1 && types[0] == want
}

func headerBudget(r *http.Request) bool {
	size := len(r.Method) + len(r.RequestURI) + len(r.Proto) + len(r.Host) + 16
	for name, values := range r.Header {
		for _, value := range values {
			size += len(name) + len(value) + 4
		}
	}
	return size <= storagedriver.MaxHeaderBytes
}

func validateRequest(err error) error {
	if err == nil {
		return nil
	}
	return rpcError(errorCode(err, storagedriver.CodeInvalidRequest))
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", storagedriver.MediaTypeJSON)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error, fallback storagedriver.ErrorCode) {
	code := errorCode(err, fallback)
	w.Header().Set("Connection", "close")
	w.Header().Set("Content-Type", storagedriver.MediaTypeJSON)
	if code == storagedriver.CodeUnauthenticated {
		w.Header().Set("WWW-Authenticate", `Bearer realm="soha-storage-driver"`)
	}
	w.WriteHeader(code.HTTPStatus())
	_ = json.NewEncoder(w).Encode(rpcError(code))
}

type uploadReader struct {
	body io.Reader
	hash hash.Hash
	size int64
	err  error
}

func (b *uploadReader) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if b.err == nil {
		b.err = err
	}
	b.size += int64(n)
	_, _ = b.hash.Write(p[:n])
	return n, err
}

func (b *uploadReader) complete() bool {
	if b.err != nil {
		return b.err == io.EOF
	}
	var probe [1]byte
	n, err := b.Read(probe[:])
	return n == 0 && err == io.EOF
}
