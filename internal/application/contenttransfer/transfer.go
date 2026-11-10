package contenttransfer

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/opensoha/soha/internal/application/contentdlp"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type scopeGate struct {
	destination d.Destination
	backend     Backend
	permission  string
}
type operation struct {
	sessionID                                      string
	nameRedacted                                   bool
	id, action, destinationID, sourceID, name, key string
	content                                        []byte
	gates                                          []scopeGate
	authorization                                  *d.Authorization
}

func (s *Service) gate(ctx context.Context, p identity.Principal, id, key string, enabled bool) (scopeGate, error) {
	dest, err := s.destination(ctx, p, id, key)
	if err != nil {
		return scopeGate{}, err
	}
	if dest.Published == nil || enabled && !dest.Published.Enabled {
		return scopeGate{}, apperrors.ErrAccessDenied
	}
	if err := ValidateConfig(*dest.Published); err != nil {
		return scopeGate{}, apperrors.ErrServiceUnavailable
	}
	backend, err := s.backends.Prepare(ctx, p, dest)
	return scopeGate{dest, backend, key}, err
}
func (s *Service) Upload(ctx context.Context, p identity.Principal, in d.UploadInput) (d.Result, error) {
	return s.submit(ctx, p, "upload", in)
}
func (s *Service) Paste(ctx context.Context, p identity.Principal, in d.PasteInput) (d.Result, error) {
	return s.submit(ctx, p, "paste", d.UploadInput{SessionID: in.SessionID, RequestID: in.RequestID, Authorization: in.Authorization, DestinationID: in.DestinationID, FileName: in.FileName, Content: []byte(in.Text)})
}
func (s *Service) submit(ctx context.Context, p identity.Principal, action string, in d.UploadInput) (d.Result, error) {
	defer clear(in.Content)
	if !validID(in.RequestID) || !validName(in.FileName) || in.Content == nil || len(in.Content) > dlp.MaxTextBytes {
		return d.Result{}, apperrors.ErrInvalidArgument
	}
	ctx, finish, err := s.operationContext(ctx)
	if err != nil {
		return d.Result{}, err
	}
	defer finish()
	gate, err := s.gate(ctx, p, in.DestinationID, Create, true)
	if err != nil {
		return d.Result{}, err
	}
	owner := sha256.Sum256([]byte(p.UserID))
	key := "content-transfers/" + in.DestinationID + "/" + hex.EncodeToString(owner[:]) + "/" + in.RequestID
	op := operation{sessionID: in.SessionID, id: in.RequestID, action: action, destinationID: in.DestinationID, name: in.FileName, content: in.Content, gates: []scopeGate{gate}, authorization: in.Authorization, key: key}
	return s.execute(ctx, p, op)
}
func fileRecord(r d.Record, p identity.Principal) bool {
	return r.ActorID == p.UserID && r.State == "completed" && r.DeletedAt == nil && (r.Action == "upload" || r.Action == "paste" || r.Action == "copy") && r.Private.ObjectKey != "" && len(r.Private.Backends) > 0
}
func (s *Service) source(ctx context.Context, p identity.Principal, id, key string, enabled bool) (d.Record, scopeGate, error) {
	if !validID(id) {
		return d.Record{}, scopeGate{}, apperrors.ErrInvalidArgument
	}
	r, err := s.store.Record(ctx, id)
	if err != nil {
		return r, scopeGate{}, err
	}
	if !fileRecord(r, p) {
		return d.Record{}, scopeGate{}, apperrors.ErrAccessDenied
	}
	gate, err := s.gate(ctx, p, r.DestinationID, key, enabled)
	if err != nil {
		return d.Record{}, gate, err
	}
	if gate.backend.Identity() != r.Private.Locator {
		return d.Record{}, gate, apperrors.ErrConflict
	}
	return r, gate, nil
}
func readContent(ctx context.Context, backend Backend, r d.Record) ([]byte, error) {
	reader, err := backend.Open(ctx, r.Private.ObjectKey)
	if err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(reader, dlp.MaxTextBytes+1))
	closed := reader.Close()
	if err != nil || closed != nil {
		clear(content)
		return nil, errors.Join(err, closed)
	}
	sum := sha256.Sum256(content)
	if len(content) > dlp.MaxTextBytes || int64(len(content)) != r.SizeBytes || hex.EncodeToString(sum[:]) != r.Private.Checksum {
		clear(content)
		return nil, apperrors.ErrConflict
	}
	return content, nil
}
func (s *Service) Read(ctx context.Context, p identity.Principal, id string, in d.Request) (d.Result, error) {
	if !validID(in.RequestID) {
		return d.Result{}, apperrors.ErrInvalidArgument
	}
	ctx, finish, err := s.operationContext(ctx)
	if err != nil {
		return d.Result{}, err
	}
	defer finish()
	source, gate, err := s.source(ctx, p, id, Download, true)
	if err != nil {
		return d.Result{}, err
	}
	content, err := readContent(ctx, gate.backend, source)
	if err != nil {
		return d.Result{}, err
	}
	defer clear(content)
	return s.execute(ctx, p, operation{sessionID: in.SessionID, id: in.RequestID, action: "download", destinationID: source.DestinationID, sourceID: id, name: source.FileName, nameRedacted: source.NameRedacted, content: content, key: source.Private.ObjectKey, gates: []scopeGate{gate}, authorization: in.Authorization})
}
func (s *Service) Copy(ctx context.Context, p identity.Principal, id string, in d.CopyInput) (d.Result, error) {
	if !validID(in.RequestID) {
		return d.Result{}, apperrors.ErrInvalidArgument
	}
	ctx, finish, err := s.operationContext(ctx)
	if err != nil {
		return d.Result{}, err
	}
	defer finish()
	source, from, err := s.source(ctx, p, id, Download, true)
	if err != nil {
		return d.Result{}, err
	}
	to, err := s.gate(ctx, p, in.DestinationID, Create, true)
	if err != nil {
		return d.Result{}, err
	}
	content, err := readContent(ctx, from.backend, source)
	if err != nil {
		return d.Result{}, err
	}
	defer clear(content)
	owner := sha256.Sum256([]byte(p.UserID))
	key := "content-transfers/" + in.DestinationID + "/" + hex.EncodeToString(owner[:]) + "/" + in.RequestID
	return s.execute(ctx, p, operation{sessionID: in.SessionID, id: in.RequestID, action: "copy", destinationID: in.DestinationID, sourceID: id, name: source.FileName, nameRedacted: source.NameRedacted, content: content, key: key, gates: []scopeGate{from, to}, authorization: in.Authorization})
}
func (s *Service) Remove(ctx context.Context, p identity.Principal, id string, in d.Request) (d.Result, error) {
	if !validID(in.RequestID) || in.Authorization != nil {
		return d.Result{}, apperrors.ErrInvalidArgument
	}
	ctx, finish, err := s.operationContext(ctx)
	if err != nil {
		return d.Result{}, err
	}
	defer finish()
	source, gate, err := s.source(ctx, p, id, Delete, false)
	if err != nil {
		return d.Result{}, err
	}
	return s.execute(ctx, p, operation{id: in.RequestID, action: "delete", destinationID: source.DestinationID, sourceID: id, name: source.FileName, nameRedacted: source.NameRedacted, key: source.Private.ObjectKey, gates: []scopeGate{gate}})
}
func operationTag(key []byte, p identity.Principal, op operation, gates []d.Gate, backends []string) []byte {
	digest := sha256.Sum256(op.content)
	material, _ := json.Marshal([]any{p.UserID, op.action, op.destinationID, op.sourceID, op.name, op.key, gates, backends, hex.EncodeToString(digest[:])})
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(material)
	return mac.Sum(nil)
}
func prepareRecord(ctx context.Context, p identity.Principal, op operation) (d.Record, error) {
	now := time.Now().UTC()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return d.Record{}, err
	}
	r := d.Record{ID: op.id, ActorID: p.UserID, DestinationID: op.destinationID, SourceID: op.sourceID, Action: op.action, FileName: op.name, SizeBytes: int64(len(op.content)), State: "prepared", NameRedacted: op.nameRedacted, Revision: 1, Gates: []d.Gate{}, AppealStatus: "none", CreatedAt: now, UpdatedAt: now, Private: d.Private{Key: key, Backends: []string{}, ObjectKey: op.key, ExpiresAt: now.Add(24 * time.Hour), Deadline: now.Add(15 * time.Second)}}
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(r.Private.Deadline) {
		r.Private.Deadline = deadline
	}
	sum := sha256.Sum256(op.content)
	r.Private.Checksum = hex.EncodeToString(sum[:])
	for _, g := range op.gates {
		r.Gates = append(r.Gates, d.Gate{DestinationID: g.destination.ID, PolicyVersion: g.destination.PublishedVersion})
		r.Private.Backends = append(r.Private.Backends, g.backend.SnapshotIdentity())
	}
	if op.action != "delete" {
		decision, redact, network := evaluateWithLink(ctx, op)
		r.Private.NetworkRequested = network
		decision.EventID = r.ID
		r.Decision = &decision
		if redact {
			r.FileName = "restricted-name.txt"
			r.NameRedacted = true
		}
		if !decision.Allowed {
			r.State = "blocked"
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(op.content)
		r.Decision.ContentFingerprint = hex.EncodeToString(mac.Sum(nil))
	}
	r.Private.Tag = operationTag(key, p, op, r.Gates, r.Private.Backends)
	r.Private.RequestTag = requestTag(key, p, op)
	r.Private.Locator = op.gates[len(op.gates)-1].backend.Identity()
	return r, nil
}
func evaluate(ctx context.Context, op operation) (dlp.Decision, bool) {
	decision, redact, _ := evaluateWithLink(ctx, op)
	return decision, redact
}
func evaluateWithLink(ctx context.Context, op operation) (dlp.Decision, bool, bool) {
	result := dlp.Decision{DataLevel: "unclassified", Mode: "audit", Status: "clean", Allowed: true, DetectorVersion: dlp.DetectorVersion, MatchedRuleIDs: []string{}}
	redact := false
	network := false
	ruleSets := []string{}
	for _, g := range op.gates {
		c := g.destination.Published.Policy.Scanner()
		decision := contentdlp.Inspect(ctx, c, op.content)
		name := contentdlp.Inspect(ctx, c, []byte(op.name))
		if name.Status != "clean" {
			redact = true
			mergeDecision(&decision, name)
		}
		ruleSets = append(ruleSets, contentdlp.RuleSetVersion(c))
		if !decision.Allowed && decision.Status == "matched" && c.Mode == "block" && c.NetworkAction == "revoke_network" {
			network = true
		}
		if !decision.Allowed {
			if decision.Status != "matched" {
				result.RequiredAction = "unsupported"
			} else if c.Mode == "block" && result.RequiredAction != "unsupported" {
				result.RequiredAction = "appeal"
			} else if result.RequiredAction == "" {
				result.RequiredAction = "acknowledge"
			}
		}
		mergeDecision(&result, decision)
		result.PolicyVersion = max(result.PolicyVersion, g.destination.PublishedVersion)
	}
	if result.RequiredAction == "unsupported" {
		result.RequiredAction = ""
	}
	if result.Status != "matched" {
		result.RequiredAction = ""
		result.StepUpRequired = false
	}
	if len(ruleSets) == 1 {
		result.RuleSetVersion = ruleSets[0]
	} else {
		raw, _ := json.Marshal(ruleSets)
		sum := sha256.Sum256(raw)
		result.RuleSetVersion = "sha256:" + hex.EncodeToString(sum[:])
	}
	if result.Status != "clean" && result.Status != "matched" {
		result.DataLevel = "unknown"
	}
	return result, redact, network && !result.Allowed && result.Status == "matched"
}
func mergeDecision(to *dlp.Decision, from dlp.Decision) {
	if from.DetectorVersion == dlp.ArchiveDetectorVersion {
		to.DetectorVersion = from.DetectorVersion
	}
	to.StepUpRequired = to.StepUpRequired || from.StepUpRequired
	to.DataLevel = contentdlp.HigherDataLevel(to.DataLevel, from.DataLevel)
	if to.MinimumDataLevel == "" {
		to.MinimumDataLevel = from.MinimumDataLevel
	}
	ranks := map[string]int{"clean": 0, "matched": 1, "unsupported": 2, "encrypted": 3, "too_large": 4, "timeout": 5, "error": 6}
	previousRank := ranks[to.Status]
	// A non-enforcing audit result must not erase an enforcing gate's match
	// and its acknowledgement, independent appeal or network response.
	if !from.Allowed && to.Allowed || from.Allowed == to.Allowed && ranks[from.Status] > ranks[to.Status] {
		to.Status = from.Status
	}
	strength := map[string]int{"audit": 0, "warn": 1, "block": 2}
	if !from.Allowed {
		if to.Allowed || strength[from.Mode] > strength[to.Mode] {
			to.Mode = from.Mode
			to.MinimumDataLevel = from.MinimumDataLevel
		}
		to.Allowed = false
	} else if to.Allowed {
		if ranks[from.Status] > previousRank || ranks[from.Status] == 0 && previousRank == 0 && strength[from.Mode] > strength[to.Mode] {
			to.Mode = from.Mode
			to.MinimumDataLevel = from.MinimumDataLevel
		}
	}
	for _, id := range from.MatchedRuleIDs {
		if !slices.Contains(to.MatchedRuleIDs, id) {
			to.MatchedRuleIDs = append(to.MatchedRuleIDs, id)
		}
	}
}

func (s *Service) operationContext(ctx context.Context) (context.Context, func(), error) {
	select {
	case s.slots <- struct{}{}:
	default:
		return nil, nil, apperrors.ErrServiceUnavailable
	}
	limited, cancel := context.WithTimeout(ctx, 15*time.Second)
	return limited, func() { cancel(); <-s.slots }, nil
}

func requestTag(key []byte, p identity.Principal, op operation) []byte {
	digest := sha256.Sum256(op.content)
	authorizationID, authorizationHash := "", ""
	if op.authorization != nil {
		authorizationID = op.authorization.AttemptID
		authorizationHash = hashToken(op.authorization.Token)
	}
	raw, _ := json.Marshal([]any{"request", op.id, p.UserID, op.action, op.destinationID, op.sourceID, op.name, hex.EncodeToString(digest[:]), authorizationID, authorizationHash})
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}
