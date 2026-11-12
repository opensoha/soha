// Package radiusruntime tracks the private EAP admission boundary of one owned
// FreeRADIUS child. It never restores State or observation certainty across epochs.
package radiusruntime

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrDenied  = errors.New("EAP admission denied")
	ErrInvalid = errors.New("invalid EAP hook input")
	ErrUnknown = errors.New("EAP State observation unknown")
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var stationPattern = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$|^([0-9a-fA-F]{2}-){5}[0-9a-fA-F]{2}$`)

type Options struct {
	Epoch                            string
	Binding                          string
	CertificateValidUntil, HardUntil time.Time
	// FreshChild and CompleteHooks are attestations from the owning supervisor,
	// not deductions from an empty map or a running Kubernetes Pod.
	FreshChild, CompleteHooks bool
	// MaxStates bounds active rounds and all distinct State values seen in this
	// epoch. Retired values are retained to prevent replay into a later round.
	MaxStates, MaxPending int
	RequestTimeout        time.Duration
	Now                   func() time.Time
	Random                io.Reader
}

type Control struct {
	Binding                           string
	Epoch                             string
	Revision                          uint64
	ConfigValidUntil, LeaseValidUntil time.Time
	Draining                          bool
	DrainUntil                        time.Time
}

type Request struct {
	Epoch     string `json:"epoch"`
	NASID     string `json:"nasId"`
	StationID string `json:"stationId"`
	State     string `json:"state"`
}

type Completion struct {
	Request
	Nonce     string `json:"nonce"`
	NextState string `json:"nextState,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
}

type Snapshot struct {
	Epoch    string `json:"epoch"`
	Revision uint64 `json:"revision"`
	Known    bool   `json:"known"`
	Active   *int   `json:"active,omitempty"`
	Pending  int    `json:"pending"`
	Draining bool   `json:"draining"`
	Reason   string `json:"reason,omitempty"`
}

type round struct {
	identity    Request
	deadline    time.Time
	state, busy string
}
type pending struct {
	request  Request
	round    *round
	deadline time.Time
}
type Tracker struct {
	mu      sync.Mutex
	options Options
	control Control
	known   bool
	reason  string
	rounds  map[*round]struct{}
	states  map[string]*round
	seen    map[string]struct{}
	pending map[string]pending
}

func New(options Options, control Control) (*Tracker, error) {
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(options.Binding) || !identifier.MatchString(options.Epoch) || options.MaxStates < 1 || options.MaxStates > 65536 || options.MaxPending < 1 || options.MaxPending > 4096 || options.RequestTimeout <= 0 || options.RequestTimeout > 5*time.Second {
		return nil, ErrInvalid
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if !options.CertificateValidUntil.After(options.Now()) || !options.HardUntil.After(options.Now()) || control.Binding != options.Binding || !validControl(options.Epoch, control, options.Now()) {
		return nil, ErrInvalid
	}
	t := &Tracker{options: options, control: control, known: options.FreshChild && options.CompleteHooks,
		rounds: map[*round]struct{}{}, states: map[string]*round{}, seen: map[string]struct{}{}, pending: map[string]pending{}}
	if !t.known {
		t.reason = "fresh_child_or_hooks_unproven"
	}
	return t, nil
}

func validControl(epoch string, control Control, now time.Time) bool {
	return control.Epoch == epoch && control.Revision > 0 && control.ConfigValidUntil.After(now) && control.LeaseValidUntil.After(now) && (!control.Draining || control.DrainUntil.After(now))
}

func canonical(request Request) (Request, error) {
	if !identifier.MatchString(request.Epoch) || !identifier.MatchString(request.NASID) || !stationPattern.MatchString(request.StationID) {
		return Request{}, ErrInvalid
	}
	request.StationID = strings.ReplaceAll(strings.ToLower(request.StationID), "-", ":")
	if request.State != "" {
		if !strings.HasPrefix(request.State, "0x") || len(request.State) < 4 || len(request.State) > 508 {
			return Request{}, ErrInvalid
		}
		raw, err := hex.DecodeString(request.State[2:])
		if err != nil || len(raw) > 253 {
			return Request{}, ErrInvalid
		}
		request.State = "0x" + hex.EncodeToString(raw)
	}
	return request, nil
}

func (t *Tracker) unknown(reason string) {
	t.known = false
	if t.reason == "" {
		t.reason = reason
	}
}
func (t *Tracker) MarkUnknown() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unknown("hook_observation_failed")
}

func (t *Tracker) sweep(now time.Time) {
	if !t.options.CertificateValidUntil.After(now) || !t.options.HardUntil.After(now) {
		t.unknown("immutable_authorization_expired")
	}
	if !t.control.ConfigValidUntil.After(now) || !t.control.LeaseValidUntil.After(now) {
		t.unknown("configuration_or_control_expired")
	}
	if t.control.Draining && !t.control.DrainUntil.After(now) && len(t.rounds) > 0 {
		t.unknown("drain_deadline_unconfirmed")
	}
	for _, p := range t.pending {
		if !p.deadline.After(now) {
			t.unknown("completion_missing")
		}
	}
	// Expiry denies future packets, but is not proof the child discarded its State.
	for r := range t.rounds {
		if !r.deadline.After(now) {
			t.unknown("state_expiry_unconfirmed")
		}
	}
}

// RenewControl refreshes only current watchdog leases. It cannot change child
// binding, intent, revision or the first fixed drain deadline, or revive uncertainty.
func (t *Tracker) RenewControl(control Control) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.options.Now()
	t.sweep(now)
	if !t.known {
		return ErrUnknown
	}
	if control.Binding != t.options.Binding || !validControl(t.options.Epoch, control, now) || control.Revision != t.control.Revision || control.Draining != t.control.Draining || !control.DrainUntil.Equal(t.control.DrainUntil) {
		return ErrDenied
	}
	t.control = control
	return nil
}

func (t *Tracker) UpdateControl(control Control) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.options.Now()
	t.sweep(now)
	if !t.known {
		return ErrUnknown
	}
	if control.Binding != t.options.Binding || !validControl(t.options.Epoch, control, now) || control.Revision <= t.control.Revision {
		return ErrInvalid
	}
	if t.control.Draining && control.Draining && control.DrainUntil.After(t.control.DrainUntil) {
		return ErrDenied
	}
	t.control = control
	for r := range t.rounds {
		if control.Draining {
			r.deadline = earliest(r.deadline, control.DrainUntil)
		}
	}
	t.sweep(now)
	if !t.known {
		return ErrUnknown
	}
	return nil
}

func earliest(values ...time.Time) time.Time {
	result := values[0]
	for _, v := range values[1:] {
		if v.Before(result) {
			result = v
		}
	}
	return result
}

func (t *Tracker) Admit(input Request) (string, error) {
	request, err := canonical(input)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.options.Now()
	t.sweep(now)
	if request.Epoch != t.options.Epoch {
		return "", ErrDenied
	}
	if !t.known {
		return "", ErrUnknown
	}
	if len(t.pending) >= t.options.MaxPending {
		return "", ErrDenied
	}
	r, err := t.admitRound(request, now)
	if err != nil {
		return "", err
	}
	var raw [32]byte
	if _, err = io.ReadFull(t.options.Random, raw[:]); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw[:])
	t.rounds[r] = struct{}{}
	r.busy = nonce
	t.pending[nonce] = pending{request: request, round: r, deadline: earliest(now.Add(t.options.RequestTimeout), r.deadline)}
	return nonce, nil
}

func (t *Tracker) admitRound(request Request, now time.Time) (*round, error) {
	if request.State == "" {
		if t.control.Draining || len(t.rounds) >= t.options.MaxStates || len(t.seen) >= t.options.MaxStates {
			return nil, ErrDenied
		}
		return &round{identity: request, deadline: earliest(now.Add(60*time.Second), t.options.CertificateValidUntil, t.options.HardUntil)}, nil
	}
	r := t.states[request.State]
	if r == nil || r.identity.NASID != request.NASID || r.identity.StationID != request.StationID || r.busy != "" {
		return nil, ErrDenied
	}
	return r, nil
}

func validNonce(nonce string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	return err == nil && len(nonce) == 43 && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == nonce
}

func (t *Tracker) completion(input Completion) (pending, error) {
	request, err := canonical(input.Request)
	if err != nil || !validNonce(input.Nonce) {
		t.unknown("completion_invalid")
		return pending{}, ErrInvalid
	}
	if request.Epoch != t.options.Epoch {
		return pending{}, ErrDenied
	}
	p, ok := t.pending[input.Nonce]
	if !ok || p.request != request {
		t.unknown("completion_conflict")
		return pending{}, ErrUnknown
	}
	t.sweep(t.options.Now())
	if !t.known {
		return pending{}, ErrUnknown
	}
	return p, nil
}

// Challenge records the final reply State. If FreeRADIUS has not supplied one,
// the returned generated State MUST be applied to reply:State by rlm_rest before
// sending. A failed HTTP write or failed attribute mapping must reject the reply.
func (t *Tracker) Challenge(input Completion) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, err := t.completion(input)
	if err != nil {
		return "", err
	}
	if input.NextState == "" {
		var raw [32]byte
		if _, err = io.ReadFull(t.options.Random, raw[:]); err != nil {
			t.unknown("challenge_random_failed")
			return "", err
		}
		input.NextState = "0x" + hex.EncodeToString(raw[:])
	}
	next, err := canonical(Request{Epoch: input.Epoch, NASID: input.NASID, StationID: input.StationID, State: input.NextState})
	if err != nil || input.Outcome != "" {
		t.unknown("challenge_state_invalid")
		return "", ErrInvalid
	}
	if next.State != p.round.state {
		if _, seen := t.seen[next.State]; seen || len(t.seen) >= t.options.MaxStates {
			t.unknown("challenge_state_reuse_or_capacity")
			return "", ErrUnknown
		}
	}
	delete(t.states, p.round.state)
	p.round.state = next.State
	p.round.busy = ""
	t.states[next.State] = p.round
	t.seen[next.State] = struct{}{}
	delete(t.pending, input.Nonce)
	return next.State, nil
}

func (t *Tracker) Finish(input Completion) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, err := t.completion(input)
	if err != nil {
		return err
	}
	if (input.Outcome != "accept" && input.Outcome != "reject") || input.NextState != "" {
		t.unknown("finish_outcome_invalid")
		return ErrInvalid
	}
	delete(t.states, p.round.state)
	delete(t.rounds, p.round)
	delete(t.pending, input.Nonce)
	return nil
}

func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(t.options.Now())
	result := Snapshot{Epoch: t.options.Epoch, Revision: t.control.Revision, Known: t.known, Pending: len(t.pending), Draining: t.control.Draining, Reason: t.reason}
	if t.known {
		active := len(t.rounds)
		result.Active = &active
	}
	return result
}
