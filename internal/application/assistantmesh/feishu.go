package assistantmesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	feishuReceiptText    = "已收到。若有可回复的内容，将通过机器人私聊发送。"
	feishuMessageAge     = 24 * time.Hour
	feishuRetryWindow    = 45 * time.Minute
	feishuPollDelay      = 5 * time.Second
	feishuAnswerPoll     = 20 * time.Second
	feishuReceiptRetries = 2
)

type FeishuJobs interface {
	EnqueueFeishuJob(context.Context, domain.FeishuJob) error
	ClaimFeishuJob(context.Context, string, time.Time) (domain.FeishuJob, error)
	SaveFeishuJob(context.Context, domain.FeishuJob) error
}

type FeishuDirectory interface {
	ResolveFeishuUser(context.Context, string) (string, error)
}

// FeishuSender makes one attempt. A temporary or ambiguous provider failure
// stays with the Core job, so every retry uses current authorization.
type FeishuSender interface {
	SendFeishuText(ctx context.Context, idType, recipient, requestID, text string) (retryable bool, err error)
}

type FeishuChannel struct {
	reception               *Reception
	jobs                    FeishuJobs
	directory               FeishuDirectory
	sender                  FeishuSender
	appID, tenantKey, botID string
	scope                   string
	now                     func() time.Time
}

func NewFeishuChannel(reception *Reception, jobs FeishuJobs, directory FeishuDirectory, sender FeishuSender,
	appID, tenantKey, botID, connectionID string,
) *FeishuChannel {
	return &FeishuChannel{
		reception: reception, jobs: jobs, directory: directory, sender: sender,
		appID: appID, tenantKey: tenantKey, botID: botID,
		scope: feishuDigest(appID + "\x00" + tenantKey + "\x00" + connectionID + "\x00" + botID),
		now:   nowUTC,
	}
}

// Enqueue receives authenticated Feishu events before the connector acknowledges
// them. Only the bounded, classified private fields are written to the inbox.
func (c *FeishuChannel) Enqueue(ctx context.Context, event domainevent.ConnectorEvent) error {
	now := c.now()
	providerTime, _ := event.Payload["providerCreateTime"].(string)
	milliseconds, err := strconv.ParseInt(providerTime, 10, 64)
	occurred := time.UnixMilli(milliseconds)
	if err != nil || occurred.Before(now.Add(-feishuMessageAge)) || occurred.After(now.Add(5*time.Minute)) {
		return nil
	}
	job, ok := parseFeishuMessage(event, c.appID, c.tenantKey, c.botID)
	if !ok {
		return nil
	}
	if job.Question != "" {
		question, err := cleanReceptionText(ctx, job.Question, 2000, false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, apperrors.ErrServiceUnavailable) {
			return err
		}
		if err != nil {
			job.Question, job.TargetID = "", ""
		} else {
			job.Question = question
		}
	}
	if job.Question != "" {
		caller, owner, err := c.feishuParticipants(ctx, job)
		if err != nil && !feishuDenied(err) {
			return err
		}
		if err == nil {
			job.CallerID, job.OwnerID = caller, owner
		} else {
			job.Question, job.TargetID = "", ""
		}
	}
	if job.Stage == domain.FeishuQuestion && job.Question == "" {
		return nil
	}
	job.Scope = c.scope
	job.ID = feishuDigest(job.Scope + "\x00" + job.ID)
	job.NextAt = now
	job.ExpiresAt = occurred.Add(retention)
	return c.jobs.EnqueueFeishuJob(ctx, job)
}

// ProcessOne performs one bounded step of a durable job. Every send is
// preceded by a persisted attempt window and uses a stable provider UUID.
func (c *FeishuChannel) ProcessOne(ctx context.Context) (bool, error) {
	now := c.now()
	job, err := c.jobs.ClaimFeishuJob(ctx, c.scope, now)
	if errors.Is(err, apperrors.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	attempt, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	switch job.Stage {
	case domain.FeishuReceipt:
		c.deliverFeishuReceipt(attempt, &job, now)
	case domain.FeishuQuestion:
		c.askFeishuQuestion(attempt, &job, now)
	case domain.FeishuAnswer:
		c.deliverFeishuAnswer(attempt, &job, now)
	default:
		finishFeishuJob(&job, "invalid_stage")
	}
	return true, c.jobs.SaveFeishuJob(ctx, job)
}

func (c *FeishuChannel) deliverFeishuReceipt(ctx context.Context, job *domain.FeishuJob, now time.Time) {
	if beginFeishuSend(job, now) {
		return
	}
	result := "receipt_failed"
	if now.Before(job.StartedAt.Add(feishuRetryWindow)) {
		retryable, err := c.sender.SendFeishuText(ctx, "chat_id", job.GroupID, feishuRequestID("receipt", job.ID), feishuReceiptText)
		if err == nil {
			result = "receipt_sent"
		} else if retryable && job.Attempts < feishuReceiptRetries {
			rescheduleFeishuJob(job, now)
			return
		}
	}
	if job.Question == "" || job.TargetID == "" {
		finishFeishuJob(job, result)
		return
	}
	advanceFeishuJob(job, domain.FeishuQuestion, now)
}

func (c *FeishuChannel) askFeishuQuestion(ctx context.Context, job *domain.FeishuJob, now time.Time) {
	caller, owner, err := c.feishuParticipants(ctx, *job)
	if err != nil {
		failOrRetryFeishuJob(job, now, err, "unbound")
		return
	}
	if caller != job.CallerID || owner != job.OwnerID {
		finishFeishuJob(job, "binding_changed")
		return
	}
	receipt, err := c.reception.Ask(ctx, identity.Principal{UserID: caller}, InquiryInput{
		OwnerID: owner, RequestID: feishuInquiryRequest(*job), Question: job.Question,
	})
	if err != nil {
		failOrRetryFeishuJob(job, now, err, "question_rejected")
		return
	}
	job.CallerID, job.OwnerID, job.InquiryID = caller, owner, receipt.ID
	advanceFeishuJob(job, domain.FeishuAnswer, now)
}

func (c *FeishuChannel) deliverFeishuAnswer(ctx context.Context, job *domain.FeishuJob, now time.Time) {
	answer, pending, err := c.authorizedFeishuAnswer(ctx, *job)
	switch {
	case err != nil:
		failOrRetryFeishuJob(job, now, err, "disclosure_denied")
	case pending:
		job.NextAt = now.Add(feishuAnswerPoll)
	case answer == "":
		finishFeishuJob(job, "answer_unavailable")
	default:
		c.sendFeishuAnswer(ctx, job, now, answer)
	}
}

func (c *FeishuChannel) authorizedFeishuAnswer(ctx context.Context, job domain.FeishuJob) (answer string, pending bool, err error) {
	caller, owner, err := c.feishuParticipants(ctx, job)
	if err != nil {
		return "", false, err
	}
	if caller != job.CallerID || owner != job.OwnerID {
		return "", false, apperrors.ErrAccessDenied
	}
	principal, err := c.reception.recheckReception(ctx, identity.Principal{UserID: caller}, owner)
	if err != nil {
		return "", false, err
	}
	item, err := c.reception.store.GetInquiry(ctx, caller, feishuInquiryRequest(job))
	if err != nil {
		return "", false, err
	}
	if item.ID != job.InquiryID || item.CallerID != caller || item.OwnerID != owner {
		return "", false, apperrors.ErrAccessDenied
	}
	switch item.Status {
	case domain.InquiryPending, domain.InquiryProcessing:
		return "", c.now().Before(item.DueAt), nil
	case domain.InquiryReplied:
		// Private human replies have no citation. Reapply the caller's
		// access to the owner's reception before external disclosure.
		facts, err := c.reception.facts(ctx, principal, owner)
		if err != nil || len(facts) == 0 {
			return "", false, err
		}
	case domain.InquiryAnswered:
	default:
		return "", false, nil
	}
	view, err := c.reception.inquiryView(ctx, principal, item)
	return view.Answer, false, err
}

func (c *FeishuChannel) sendFeishuAnswer(ctx context.Context, job *domain.FeishuJob, now time.Time, answer string) {
	if beginFeishuSend(job, now) {
		return
	}
	if !now.Before(job.StartedAt.Add(feishuRetryWindow)) {
		finishFeishuJob(job, "answer_unconfirmed")
		return
	}
	retryable, err := c.sender.SendFeishuText(ctx, "open_id", job.SenderID, feishuRequestID("answer", job.Scope+"\x00"+job.InquiryID), answer)
	switch {
	case err == nil:
		finishFeishuJob(job, "answer_sent")
	case retryable:
		rescheduleFeishuJob(job, now)
	default:
		finishFeishuJob(job, "answer_failed")
	}
}

func (c *FeishuChannel) feishuParticipants(ctx context.Context, job domain.FeishuJob) (caller, owner string, err error) {
	caller, err = c.directory.ResolveFeishuUser(ctx, job.SenderID)
	if err != nil {
		return "", "", err
	}
	owner, err = c.directory.ResolveFeishuUser(ctx, job.TargetID)
	if err != nil {
		return "", "", err
	}
	if caller == owner || !validReceptionID(caller) || !validReceptionID(owner) {
		return "", "", apperrors.ErrAccessDenied
	}
	return caller, owner, nil
}

func feishuInquiryRequest(job domain.FeishuJob) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("heya.feishu:"+job.ID)).String()
}

func feishuDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func feishuRequestID(kind, value string) string {
	return "heya-" + feishuDigest(kind + "\x00" + value)[:32]
}

func beginFeishuSend(job *domain.FeishuJob, now time.Time) bool {
	if !job.StartedAt.IsZero() {
		return false
	}
	job.StartedAt = now
	return true
}

func advanceFeishuJob(job *domain.FeishuJob, stage string, now time.Time) {
	job.Stage, job.NextAt, job.StartedAt, job.Attempts = stage, now, time.Time{}, 0
}

func finishFeishuJob(job *domain.FeishuJob, outcome string) {
	job.Stage, job.Outcome = domain.FeishuDone, outcome
}

func failOrRetryFeishuJob(job *domain.FeishuJob, now time.Time, err error, outcome string) {
	if !feishuDenied(err) && job.Attempts < 10 {
		rescheduleFeishuJob(job, now)
		return
	}
	finishFeishuJob(job, outcome)
}

func feishuDenied(err error) bool {
	return errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrUnauthorized) ||
		errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrConflict) ||
		errors.Is(err, apperrors.ErrInvalidArgument) || errors.Is(err, apperrors.ErrGone) ||
		errors.Is(err, errSensitiveInquiry)
}

func rescheduleFeishuJob(job *domain.FeishuJob, now time.Time) {
	job.NextAt = now.Add(feishuPollDelay << min(job.Attempts, 6))
	job.Attempts++
}
