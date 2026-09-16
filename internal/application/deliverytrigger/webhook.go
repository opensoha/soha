package deliverytrigger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var eventCommitPattern = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func decodeSigningSecret(value string) ([]byte, error) {
	if !strings.HasPrefix(value, "whsec_") || len(value) > 2000 {
		return nil, fmt.Errorf("%w: use a Standard Webhooks signing token", apperrors.ErrInvalidArgument)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "whsec_"))
	if err != nil || len(key) < 32 {
		return nil, fmt.Errorf("%w: signing token must contain at least 32 random bytes", apperrors.ErrInvalidArgument)
	}
	return key, nil
}

func verifyWebhook(secret, id, timestamp, signature string, body []byte, now time.Time) (time.Time, error) {
	if id == "" || len(id) > 200 || len(timestamp) > 12 || len(signature) > 2000 || len(body) > 1<<20 || strings.ContainsAny(id, "\x00\r\n") {
		return time.Time{}, apperrors.ErrUnauthorized
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return time.Time{}, apperrors.ErrUnauthorized
	}
	at := time.Unix(seconds, 0).UTC()
	if at.Before(now.Add(-5*time.Minute)) || at.After(now.Add(5*time.Minute)) {
		return at, apperrors.ErrUnauthorized
	}
	key, err := decodeSigningSecret(secret)
	if err != nil {
		return at, apperrors.ErrUnauthorized
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + timestamp + "."))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	for _, value := range strings.Fields(signature) {
		version, encoded, ok := strings.Cut(value, ",")
		if !ok || version != "v1" {
			continue
		}
		actual, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil && hmac.Equal(actual, expected) {
			return at, nil
		}
	}
	return at, apperrors.ErrUnauthorized
}

type gitLabPush struct {
	Kind    string `json:"object_kind"`
	Ref     string `json:"ref"`
	After   string `json:"after"`
	Project struct {
		ID int64 `json:"id"`
	} `json:"project"`
}

func (s *Service) ReceiveWebhook(ctx context.Context, id, eventID, timestamp, signature string, body []byte) (domain.Event, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Event{}, err
	}
	if !item.Enabled || item.Type != "webhook" || item.Webhook == nil {
		return domain.Event{}, apperrors.ErrAccessDenied
	}
	now := time.Now().UTC()
	at, err := verifyWebhook(item.SigningSecret, eventID, timestamp, signature, body, now)
	if err != nil {
		return domain.Event{}, err
	}
	var payload gitLabPush
	if err := json.Unmarshal(body, &payload); err != nil || payload.Kind != "push" && payload.Kind != "tag_push" || !eventCommitPattern.MatchString(payload.After) || payload.Project.ID < 1 {
		return domain.Event{}, apperrors.ErrInvalidArgument
	}
	refPrefix := "refs/heads/"
	kind := "push"
	if item.Webhook.RefType == "tag" {
		refPrefix, kind = "refs/tags/", "tag_push"
	}
	if payload.Kind != kind || payload.Ref != refPrefix+item.Webhook.RefValue {
		return domain.Event{}, fmt.Errorf("%w: webhook repository ref does not match", apperrors.ErrInvalidArgument)
	}
	executor, err := s.identity.CurrentExecutionPrincipal(ctx, "service_account:"+item.ServiceAccountID, item.ExecutionTokenID)
	if err != nil {
		return domain.Event{}, err
	}
	repository, err := s.repositories.GetRepository(ctx, executor, item.Webhook.RepositoryID)
	if err != nil {
		return domain.Event{}, err
	}
	if repository.Provider != "gitlab" || repository.ProviderRepositoryID != strconv.FormatInt(payload.Project.ID, 10) {
		return domain.Event{}, apperrors.ErrAccessDenied
	}
	sum := sha256.Sum256(body)
	event := domain.StoredEvent{Event: domain.Event{ID: uuid.NewString(), TriggerID: item.ID, TriggerRevision: item.Revision, EventID: eventID, EventType: "webhook", Status: "queued", ResolvedCommit: payload.After, OccurredAt: at, CreatedAt: now, UpdatedAt: now}, PayloadDigest: "sha256:" + hex.EncodeToString(sum[:])}
	if strings.Trim(payload.After, "0") == "" {
		event.Status, event.Reason = "skipped", "ref_deleted"
	}
	return s.repo.Enqueue(ctx, event)
}
