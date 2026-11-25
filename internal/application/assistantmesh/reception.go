package assistantmesh

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ReceptionStore interface {
	WithReceptionTransaction(context.Context, func(context.Context) error) error
	GetProfile(context.Context, string) (domain.Profile, error)
	PutProfile(context.Context, domain.Profile, time.Time, time.Duration) error
	ListProfiles(context.Context, string, int) ([]domain.Profile, error)
	GetInquiry(context.Context, string, string) (domain.Inquiry, error)
	CreateInquiry(context.Context, domain.Inquiry, domain.InquiryLimits) (domain.Inquiry, error)
	CompleteInquiry(context.Context, domain.Inquiry) error
	FallbackInquiry(context.Context, domain.Inquiry, int, time.Time, time.Duration) (domain.Inquiry, error)
	ListInquiries(context.Context, string, bool, time.Time, time.Duration) ([]domain.Inquiry, error)
	ReplyInquiry(context.Context, string, string, string, time.Time, time.Duration) (domain.Inquiry, error)
}

type ReceptionIdentity interface {
	CurrentPrincipal(context.Context, string) (identity.Principal, error)
}

type ReceptionAccess interface {
	GetPersonalAgentAccess(context.Context, identity.Principal) (domaincopilot.PersonalAgentAccess, error)
}

type Reception struct {
	assets     *Service
	store      ReceptionStore
	identities ReceptionIdentity
	access     ReceptionAccess
	model      ReceptionModel
}

func NewReception(assets *Service, store ReceptionStore, identities ReceptionIdentity, access ReceptionAccess, model ReceptionModel) *Reception {
	return &Reception{assets: assets, store: store, identities: identities, access: access, model: model}
}

const (
	retentionDays  = 7
	retention      = retentionDays * 24 * time.Hour
	discoveryLimit = 30
	pageSize       = 100
)

type ReceptionProfile struct {
	domain.Profile
	RetentionDays int `json:"retentionDays"`
}

type DiscoveredAssistant struct {
	OwnerID string   `json:"ownerId"`
	Name    string   `json:"name"`
	Intro   string   `json:"intro"`
	Away    bool     `json:"away"`
	Topics  []string `json:"topics"`
}

type QuestionPreview struct {
	Question string `json:"question"`
	Blocked  bool   `json:"blocked"`
}

type InquiryView struct {
	domain.Inquiry
	CallerName string `json:"callerName"`
	OwnerName  string `json:"ownerName"`
}

func (s *Reception) prepare(ctx context.Context, p identity.Principal) (identity.Principal, error) {
	if p.AccessTokenID != "" || p.GovernedAI != nil {
		return identity.Principal{}, apperrors.ErrAccessDenied
	}
	current, err := s.identities.CurrentPrincipal(ctx, p.UserID)
	if err != nil {
		return identity.Principal{}, err
	}
	if _, err = s.assets.prepare(ctx, current); err != nil {
		return identity.Principal{}, err
	}
	if err = s.requireHeya(ctx, current); err != nil {
		return identity.Principal{}, err
	}
	return current, nil
}

func (s *Reception) requireHeya(ctx context.Context, p identity.Principal) error {
	access, err := s.access.GetPersonalAgentAccess(ctx, p)
	if err != nil {
		return err
	}
	if access.Status != "approved" {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (s *Reception) Profile(ctx context.Context, p identity.Principal) (ReceptionProfile, error) {
	if _, err := s.prepare(ctx, p); err != nil {
		return ReceptionProfile{}, err
	}
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return ReceptionProfile{}, err
	}
	profile, err := s.store.GetProfile(ctx, p.UserID)
	return ReceptionProfile{Profile: profile, RetentionDays: int(policy.Limits.Retention / (24 * time.Hour))}, err
}

func (s *Reception) SaveProfile(ctx context.Context, p identity.Principal, profile domain.Profile) (ReceptionProfile, error) {
	if _, err := s.prepare(ctx, p); err != nil {
		return ReceptionProfile{}, err
	}
	intro, err := cleanReceptionText(ctx, profile.Intro, 280, true)
	if err != nil {
		return ReceptionProfile{}, err
	}
	profile.OwnerID, profile.Intro = p.UserID, intro
	policy, err := s.assets.InquiryPolicy(ctx)
	if err != nil {
		return ReceptionProfile{}, err
	}
	if err := s.store.PutProfile(ctx, profile, nowUTC(), policy.Limits.Retention); err != nil {
		return ReceptionProfile{}, err
	}
	_ = s.assets.audit(ctx, p, "reception.profile", p.UserID, "success", map[string]any{"open": profile.Open})
	return ReceptionProfile{Profile: profile, RetentionDays: int(policy.Limits.Retention / (24 * time.Hour))}, nil
}

func (s *Reception) openOwner(ctx context.Context, ownerID string) (identity.Principal, error) {
	profile, err := s.store.GetProfile(ctx, ownerID)
	if err != nil {
		return identity.Principal{}, err
	}
	if !profile.Open {
		return identity.Principal{}, apperrors.ErrNotFound
	}
	return s.discoverableOwner(ctx, ownerID)
}

func (s *Reception) discoverableOwner(ctx context.Context, ownerID string) (identity.Principal, error) {
	owner, err := s.identities.CurrentPrincipal(ctx, ownerID)
	if err == nil {
		err = s.requireHeya(ctx, owner)
	}
	if errors.Is(err, apperrors.ErrUnauthorized) || errors.Is(err, apperrors.ErrAccessDenied) {
		return identity.Principal{}, apperrors.ErrNotFound
	}
	return owner, err
}

func (s *Reception) Discover(ctx context.Context, p identity.Principal, query string) ([]DiscoveredAssistant, error) {
	p, err := s.prepare(ctx, p)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(query) > 120 {
		return nil, fmt.Errorf("%w: discovery query is too long", apperrors.ErrInvalidArgument)
	}
	query = strings.ToLower(strings.TrimSpace(query))
	results := make([]DiscoveredAssistant, 0)
	for after := ""; len(results) < discoveryLimit; {
		profiles, err := s.store.ListProfiles(ctx, after, pageSize)
		if err != nil {
			return nil, err
		}
		for _, profile := range profiles {
			if profile.OwnerID == p.UserID {
				continue
			}
			item, err := s.discoveryItem(ctx, p, profile, query)
			if err != nil {
				return nil, err
			}
			if item != nil {
				results = append(results, *item)
			}
			if len(results) == discoveryLimit {
				return results, nil
			}
		}
		if len(profiles) < pageSize {
			break
		}
		after = profiles[len(profiles)-1].OwnerID
	}
	return results, nil
}

func (s *Reception) discoveryItem(ctx context.Context, p identity.Principal, profile domain.Profile, query string) (*DiscoveredAssistant, error) {
	owner, err := s.discoverableOwner(ctx, profile.OwnerID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	facts, err := s.facts(ctx, p, profile.OwnerID)
	if err != nil || len(facts) == 0 {
		return nil, err
	}
	topics := make([]string, 0, len(facts))
	for _, fact := range facts {
		topics = append(topics, fact.Citation.Title)
	}
	sort.Strings(topics)
	target := strings.ToLower(strings.Join(append([]string{owner.UserName, profile.Intro}, topics...), " "))
	if !strings.Contains(target, query) && !matchesReceptionTopic(query, topics) {
		return nil, nil
	}
	return &DiscoveredAssistant{OwnerID: owner.UserID, Name: owner.UserName, Intro: profile.Intro, Away: profile.Away, Topics: topics}, nil
}

func (s *Reception) Preview(ctx context.Context, p identity.Principal, question string) (QuestionPreview, error) {
	if _, err := s.prepare(ctx, p); err != nil {
		return QuestionPreview{}, err
	}
	question = strings.TrimSpace(question)
	_, err := cleanReceptionText(ctx, question, 2000, false)
	if errors.Is(err, errSensitiveInquiry) {
		return QuestionPreview{Blocked: true}, nil
	}
	return QuestionPreview{Question: question}, err
}

func validReceptionID(id string) bool {
	value, err := uuid.Parse(id)
	return err == nil && value.String() == id
}
