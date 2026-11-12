package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxypolicy"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type SourceStore interface {
	GetSource(context.Context, string, string) (domain.SourceRecord, error)
	ListSources(context.Context, string) ([]domain.SourceRecord, error)
	SaveSource(context.Context, domain.SourceRecord, int64) error
	DeleteSource(context.Context, string, string, int64) error
	AllowedSourceNodes(context.Context, string, string, int64, []string) ([]string, error)
}
type Authorizer interface {
	Authorize(context.Context, identity.Principal, string, string) error
}
type Audit interface {
	RecordPublication(context.Context, identity.Principal, string, string, string, int64) error
}
type Compiler interface {
	Compile(api.NetworkProxySourceInput) (domain.IR, error)
}
type Fetcher interface {
	Fetch(context.Context, string) (string, error)
}
type Sources struct {
	store    SourceStore
	auth     Authorizer
	audit    Audit
	compiler Compiler
	fetch    Fetcher
	keys     keyring.Ring
	now      func() time.Time
}

func absent(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func NewSources(store SourceStore, auth Authorizer, audit Audit, compiler Compiler, fetch Fetcher, keys keyring.Ring) (*Sources, error) {
	for _, v := range []any{store, auth, audit, compiler, fetch} {
		if absent(v) {
			return nil, errors.New("proxy source dependency required")
		}
	}
	if keys.Active().ID() == "" {
		return nil, errors.New("proxy source encryption key required")
	}
	return &Sources{store: store, auth: auth, audit: audit, compiler: compiler, fetch: fetch, keys: keys, now: time.Now}, nil
}
func (s *Sources) List(ctx context.Context, p identity.Principal, instance string) (api.NetworkProxySourceList, error) {
	out := api.NetworkProxySourceList{Items: []api.NetworkProxySource{}}
	if err := s.auth.Authorize(ctx, p, instance, "view"); err != nil {
		return out, err
	}
	records, err := s.store.ListSources(ctx, instance)
	if err != nil {
		return out, err
	}
	for _, r := range records {
		out.Items = append(out.Items, r.Public)
	}
	return out, nil
}
func (s *Sources) Get(ctx context.Context, p identity.Principal, instance, id string) (api.NetworkProxySource, error) {
	if err := s.auth.Authorize(ctx, p, instance, "view"); err != nil {
		return api.NetworkProxySource{}, err
	}
	r, err := s.store.GetSource(ctx, instance, id)
	return r.Public, err
}
func validID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	first := s[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') && (first < '0' || first > '9') {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._:-", r) {
			return false
		}
	}
	return true
}
func (s *Sources) resolve(ctx context.Context, input api.NetworkProxySourceInput) (domain.IR, error) {
	if input.URL != "" {
		if input.Content != "" || len(input.Nodes) > 0 || input.Format == "manual" {
			return domain.IR{}, domain.Invalid("ambiguous_source", "url")
		}
		content, err := s.fetch.Fetch(ctx, input.URL)
		if err != nil {
			return domain.IR{}, err
		}
		input.Content = content
		input.URL = ""
	}
	return s.compiler.Compile(input)
}
func (s *Sources) Preview(ctx context.Context, p identity.Principal, instance string, input api.NetworkProxySourceInput) (api.NetworkProxyConversionPreview, error) {
	out := api.NetworkProxyConversionPreview{Errors: []api.NetworkProxyConversionError{}, Groups: []api.NetworkProxyGroup{}, SourceRevision: input.ExpectedRevision}
	out.Nodes = make([]struct {
		Label    string                                         `json:"label"`
		NodeID   string                                         `json:"nodeId"`
		Protocol api.NetworkProxyConversionPreviewNodesProtocol `json:"protocol"`
	}, 0)
	if err := s.auth.Authorize(ctx, p, instance, "update"); err != nil {
		return out, err
	}
	ir, err := s.resolve(ctx, input)
	if err != nil {
		var conversion *domain.ConversionError
		if errors.As(err, &conversion) {
			out.Errors = append(out.Errors, api.NetworkProxyConversionError{Code: api.NetworkProxyConversionErrorCode(conversion.Code), Location: conversion.Position, Message: "Source conversion rejected"})
			return out, nil
		}
		return out, err
	}
	out.Supported = true
	if ir.Groups != nil {
		out.Groups = ir.Groups
	}
	for _, n := range ir.Nodes {
		raw, _ := json.Marshal(map[string]string{"nodeId": n.NodeID, "label": n.Label, "protocol": string(n.Protocol)})
		var item struct {
			Label    string                                         `json:"label"`
			NodeID   string                                         `json:"nodeId"`
			Protocol api.NetworkProxyConversionPreviewNodesProtocol `json:"protocol"`
		}
		_ = json.Unmarshal(raw, &item)
		out.Nodes = append(out.Nodes, item)
	}
	return out, nil
}
func (s *Sources) Put(ctx context.Context, p identity.Principal, instance, id string, input api.NetworkProxySourceInput) (api.NetworkProxySource, error) {
	var out api.NetworkProxySource
	if err := s.auth.Authorize(ctx, p, instance, "update"); err != nil {
		return out, err
	}
	if !validID(instance) || !validID(id) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || input.ExpectedRevision < 0 {
		return out, domain.Invalid("invalid_source", "source")
	}
	if input.DeliveryMode == "" {
		input.DeliveryMode = "direct_subscription"
	}
	switch input.DeliveryMode {
	case "mapped_nodes", "gateway", "direct_subscription":
	default:
		return out, domain.Invalid("unsupported_delivery_mode", "deliveryMode")
	}
	if err := s.restoreSourceInput(ctx, instance, id, &input); err != nil {
		return out, err
	}
	if err := s.restoreManualCredentials(ctx, instance, id, &input); err != nil {
		return out, err
	}
	ir, err := s.resolve(ctx, input)
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(input)
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(raw))
	if err != nil {
		return out, apperrors.ErrServiceUnavailable
	}
	raw, _ = json.Marshal(ir)
	encryptedIR, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(raw))
	if err != nil {
		return out, apperrors.ErrServiceUnavailable
	}
	out = api.NetworkProxySource{SourceID: id, Name: input.Name, Format: input.Format, DeliveryMode: input.DeliveryMode, NodeCount: int64(len(ir.Nodes)), Configured: true, Revision: input.ExpectedRevision + 1, UpdatedAt: s.now().UTC()}
	if err = s.store.SaveSource(ctx, domain.SourceRecord{InstanceID: instance, Public: out, EncryptedInput: encrypted, EncryptedIR: encryptedIR, NodeMaterial: domain.NodeMaterial(ir)}, input.ExpectedRevision); err != nil {
		return api.NetworkProxySource{}, err
	}
	if err = s.audit.RecordPublication(ctx, p, instance, "source_updated", id, out.Revision); err != nil {
		return api.NetworkProxySource{}, err
	}
	return out, nil
}
func (s *Sources) Delete(ctx context.Context, p identity.Principal, instance, id string, revision int64) error {
	if err := s.auth.Authorize(ctx, p, instance, "update"); err != nil {
		return err
	}
	if revision < 1 {
		return domain.Invalid("invalid_revision", "expectedRevision")
	}
	if err := s.store.DeleteSource(ctx, instance, id, revision); err != nil {
		return err
	}
	return s.audit.RecordPublication(ctx, p, instance, "source_deleted", id, revision)
}

func (s *Sources) RuleTargets(ctx context.Context, p identity.Principal, instance string) ([]proxypolicy.Target, error) {
	if err := s.auth.Authorize(ctx, p, instance, "view"); err != nil {
		return nil, err
	}
	sources, err := s.store.ListSources(ctx, instance)
	if err != nil {
		return nil, err
	}
	targets := []proxypolicy.Target{}
	seen := map[string]bool{}
	for _, source := range sources {
		plain, e := secretcrypto.DecryptStringWithKeyring(s.keys, source.EncryptedIR)
		if e != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		var ir domain.IR
		if json.Unmarshal([]byte(plain), &ir) != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		for _, node := range ir.Nodes {
			if !seen[node.NodeID] {
				seen[node.NodeID] = true
				targets = append(targets, proxypolicy.Target{ID: node.NodeID, Kind: "node"})
			}
		}
		for _, group := range ir.Groups {
			if !seen[group.GroupID] {
				seen[group.GroupID] = true
				targets = append(targets, proxypolicy.Target{ID: group.GroupID, Kind: "group"})
			}
		}
	}
	return targets, nil
}

func preserveNodeCredentials(nodes, old []api.NetworkProxyNode) {
	for i := range nodes {
		for _, previous := range old {
			if nodes[i].Protocol != previous.Protocol || !strings.EqualFold(nodes[i].Server, previous.Server) || nodes[i].Port != previous.Port || !reflect.DeepEqual(nodes[i].TLS, previous.TLS) || !reflect.DeepEqual(nodes[i].Transport, previous.Transport) {
				continue
			}
			if nodes[i].Secret == "" {
				nodes[i].Secret = previous.Secret
			}
			if nodes[i].Username == "" {
				nodes[i].Username = previous.Username
			}
			if nodes[i].UUID == uuid.Nil {
				nodes[i].UUID = previous.UUID
			}
			break
		}
	}
}

func (s *Sources) restoreSourceInput(ctx context.Context, instance, id string, input *api.NetworkProxySourceInput) error {
	if input.Content == "" && input.URL == "" && len(input.Nodes) == 0 && input.ExpectedRevision > 0 {
		old, err := s.store.GetSource(ctx, instance, id)
		if err != nil {
			return err
		}
		if old.Public.Revision != input.ExpectedRevision {
			return apperrors.ErrConflict
		}
		plain, err := secretcrypto.DecryptStringWithKeyring(s.keys, old.EncryptedInput)
		if err != nil {
			return apperrors.ErrServiceUnavailable
		}
		var previous api.NetworkProxySourceInput
		if json.Unmarshal([]byte(plain), &previous) != nil {
			return apperrors.ErrServiceUnavailable
		}
		if previous.Format != input.Format {
			return domain.Invalid("content_required", "format")
		}
		input.Content = previous.Content
		input.URL = previous.URL
		input.Nodes = previous.Nodes
	}

	return nil
}

func (s *Sources) restoreManualCredentials(ctx context.Context, instance, id string, input *api.NetworkProxySourceInput) error {
	if input.Format == "manual" && input.ExpectedRevision > 0 && len(input.Nodes) > 0 {
		previous, readErr := s.store.GetSource(ctx, instance, id)
		if readErr != nil {
			return readErr
		}
		if previous.Public.Revision != input.ExpectedRevision {
			return apperrors.ErrConflict
		}
		plain, decryptErr := secretcrypto.DecryptStringWithKeyring(s.keys, previous.EncryptedIR)
		if decryptErr != nil {
			return apperrors.ErrServiceUnavailable
		}
		var old domain.IR
		if json.Unmarshal([]byte(plain), &old) != nil {
			return apperrors.ErrServiceUnavailable
		}
		preserveNodeCredentials(input.Nodes, old.Nodes)
	}

	return nil
}
