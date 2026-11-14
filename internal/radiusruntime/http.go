package radiusruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"
)

// PeerUID must read kernel credentials from this accepted Unix connection.
// H1 owns the platform-specific extractor and private socket lifecycle; HTTP
// headers and JSON must never be used as substitutes for kernel peer identity.
type PeerUID func(net.Conn) (uint32, error)
type peerKey struct{}
type HTTP struct {
	tracker *Tracker
	uid     uint32
	peer    PeerUID
	slots   chan struct{}
	managed *managedHook
}

type managedHook struct {
	lock     sync.Locker
	proof    func() (string, error)
	admit    func(Request) error
	observed func()
}

func (h *HTTP) withManaged(lock sync.Locker, proof func() (string, error), admit func(Request) error, observed func()) error {
	if lock == nil || proof == nil || admit == nil || observed == nil {
		return ErrInvalid
	}
	h.managed = &managedHook{lock: lock, proof: proof, admit: admit, observed: observed}
	return nil
}

func NewHTTP(tracker *Tracker, uid uint32, peer PeerUID, concurrency int) (*HTTP, error) {
	if tracker == nil || peer == nil || concurrency < 1 || concurrency > 128 {
		return nil, ErrInvalid
	}
	return &HTTP{tracker: tracker, uid: uid, peer: peer, slots: make(chan struct{}, concurrency)}, nil
}

// Server returns a bounded server but does not listen or create a socket.
// Serve only a supervisor-created Unix listener in a private directory.
func (h *HTTP) Server() *http.Server {
	return &http.Server{Handler: h, ConnContext: h.connContext, ReadHeaderTimeout: time.Second,
		ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
}
func (h *HTTP) connContext(ctx context.Context, conn net.Conn) context.Context {
	if _, ok := conn.(*net.UnixConn); !ok {
		return ctx
	}
	uid, err := h.peer(conn)
	if err != nil || uid != h.uid {
		return ctx
	}
	return context.WithValue(ctx, peerKey{}, true)
}

func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if trusted, _ := r.Context().Value(peerKey{}).(bool); !trusted {
		http.Error(w, "untrusted peer", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.Error(w, "invalid hook method", http.StatusMethodNotAllowed)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "hook busy", http.StatusServiceUnavailable)
		return
	}
	content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" {
		h.invalid(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	input, err := decode(r.Body)
	if err != nil || r.Context().Err() != nil {
		h.invalid(w, r)
		return
	}
	if h.managed != nil {
		h.managed.lock.Lock()
		defer h.managed.lock.Unlock()
	}
	switch r.URL.Path {
	case "/admit":
		h.admit(w, input)
	case "/challenge":
		h.challenge(w, input)
	case "/finish":
		h.finish(w, input)
	default:
		http.NotFound(w, r)
	}
}

func decode(body io.Reader) (map[string]json.RawMessage, error) {
	// Reject duplicate keys instead of accepting encoding/json last-value wins.
	decoder := json.NewDecoder(body)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, ErrInvalid
		}
		if _, exists := fields[key]; exists {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	return fields, nil
}

func inputRequest(fields map[string]json.RawMessage, extra ...string) (Completion, error) {
	allowed := map[string]bool{"epoch": true, "nasId": true, "stationId": true, "state": true}
	for _, key := range extra {
		allowed[key] = true
	}
	if len(fields) != len(allowed) {
		return Completion{}, ErrInvalid
	}
	for key, value := range fields {
		if !allowed[key] || string(value) == "null" {
			return Completion{}, ErrInvalid
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return Completion{}, ErrInvalid
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return Completion{}, err
	}
	var input Completion
	if err = json.Unmarshal(encoded, &input); err != nil {
		return Completion{}, err
	}
	return input, nil
}
func (h *HTTP) invalid(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/challenge" || r.URL.Path == "/finish" {
		h.tracker.MarkUnknown()
	}
	http.Error(w, "invalid hook input", http.StatusBadRequest)
}
func (h *HTTP) reply(w http.ResponseWriter, attribute, value string) bool {
	response := map[string]any{}
	if attribute != "" {
		response[attribute] = map[string]any{"value": []string{value}, "op": ":=", "do_xlat": false}
	}
	if h.managed != nil {
		proof, err := h.managed.proof()
		if err != nil || proof == "" {
			h.tracker.MarkUnknown()
			hookError(w, ErrUnknown)
			return false
		}
		response["control:Tmp-String-9"] = map[string]any{"value": []string{proof}, "op": ":=", "do_xlat": false}
	}
	w.Header().Set("Content-Type", "application/json")
	if json.NewEncoder(w).Encode(response) != nil {
		h.tracker.MarkUnknown()
		return false
	}
	return true
}

func hookError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, ErrInvalid) {
		code = http.StatusBadRequest
	}
	if errors.Is(err, ErrDenied) {
		code = http.StatusForbidden
	}
	http.Error(w, "EAP hook denied", code)
}
func (h *HTTP) admit(w http.ResponseWriter, fields map[string]json.RawMessage) {
	input, err := inputRequest(fields)
	if err != nil {
		hookError(w, err)
		return
	}
	if h.managed != nil {
		if err = h.managed.admit(input.Request); err != nil {
			hookError(w, err)
			return
		}
	}
	nonce, err := h.tracker.Admit(input.Request)
	if err != nil {
		hookError(w, err)
		return
	}
	if h.reply(w, "control:Tmp-String-8", nonce) && h.managed != nil {
		h.managed.observed()
	}
}
func (h *HTTP) challenge(w http.ResponseWriter, fields map[string]json.RawMessage) {
	input, err := inputRequest(fields, "nonce", "nextState")
	if err != nil {
		h.tracker.MarkUnknown()
		hookError(w, err)
		return
	}
	state, err := h.tracker.Challenge(input)
	if err != nil {
		hookError(w, err)
		return
	}
	if input.NextState == "" {
		h.reply(w, "reply:State", state)
		return
	}
	if h.managed != nil {
		h.reply(w, "", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *HTTP) finish(w http.ResponseWriter, fields map[string]json.RawMessage) {
	input, err := inputRequest(fields, "nonce", "outcome")
	if err != nil {
		h.tracker.MarkUnknown()
		hookError(w, err)
		return
	}
	if err = h.tracker.Finish(input); err != nil {
		hookError(w, err)
		return
	}
	if h.managed != nil {
		h.reply(w, "", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
