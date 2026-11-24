package assistantmesh

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type fakeFeishuJobs struct{ job domain.FeishuJob }

func (f *fakeFeishuJobs) EnqueueFeishuJob(_ context.Context, job domain.FeishuJob) error {
	f.job = job
	return nil
}

func (f *fakeFeishuJobs) ClaimFeishuJob(_ context.Context, scope string, now time.Time) (domain.FeishuJob, error) {
	if f.job.Scope != scope || f.job.Stage == domain.FeishuDone || f.job.NextAt.After(now) {
		return domain.FeishuJob{}, apperrors.ErrNotFound
	}
	return f.job, nil
}

func (f *fakeFeishuJobs) SaveFeishuJob(_ context.Context, job domain.FeishuJob) error {
	f.job = job
	return nil
}

type fakeFeishuDirectory map[string]string

func (f fakeFeishuDirectory) ResolveFeishuUser(_ context.Context, openID string) (string, error) {
	user, ok := f[openID]
	if !ok {
		return "", apperrors.ErrNotFound
	}
	return user, nil
}

type sentFeishuMessage struct{ idType, recipient, requestID, text string }

type fakeFeishuSender struct {
	messages  []sentFeishuMessage
	retryable bool
	err       error
}

func (f *fakeFeishuSender) SendFeishuText(_ context.Context, idType, recipient, requestID, text string) (bool, error) {
	f.messages = append(f.messages, sentFeishuMessage{idType, recipient, requestID, text})
	if idType == "open_id" {
		return f.retryable, f.err
	}
	return false, nil
}

type feishuTestSetup struct {
	channel   *FeishuChannel
	jobs      *fakeFeishuJobs
	sender    *fakeFeishuSender
	directory fakeFeishuDirectory
	reception *Reception
	inquiries *receptionTestStore
	ids       receptionTestIdentities
	settings  *fakeSettings
}

func setupFeishuTest(t *testing.T, stage, senderID, question string) feishuTestSetup {
	t.Helper()
	f := newFixture(t)
	caller, owner, other := employee(receptionCaller, "team-a"), employee(receptionOwner), employee(receptionOther)
	ids := receptionTestIdentities{}
	for _, p := range []identity.Principal{caller, owner, other} {
		f.users.users[p.UserID] = identity.User{ID: p.UserID, Status: "active"}
		ids[p.UserID] = p
	}
	store := &receptionTestStore{profile: domain.Profile{OwnerID: owner.UserID, Open: true}, inquiries: map[string]domain.Inquiry{}}
	reception := NewReception(f.service, store, ids, receptionTestAccess{}, &receptionTestModel{})
	publishReceptionFact(t, f, "shared", "订单同步", "受限排查步骤", knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-a"}}, nil)

	jobs := &fakeFeishuJobs{}
	sender := &fakeFeishuSender{}
	directory := fakeFeishuDirectory{"ou_caller": caller.UserID, "ou_owner": owner.UserID, "ou_other": other.UserID}
	channel := NewFeishuChannel(reception, jobs, directory, sender, "cli_app", "tenant", "ou_bot", "directory")
	now := time.Now().UTC()
	channel.now = func() time.Time { return now }
	jobs.job = domain.FeishuJob{
		Scope: channel.scope, ID: "event-id", Stage: stage, SenderID: senderID,
		TargetID: "ou_owner", Question: question, NextAt: now,
		CallerID: directory[senderID], OwnerID: owner.UserID,
	}
	if stage == domain.FeishuReceipt {
		jobs.job.GroupID = "oc_group"
	}
	return feishuTestSetup{
		channel: channel, jobs: jobs, sender: sender, directory: directory,
		reception: reception, inquiries: store, ids: ids, settings: f.settings,
	}
}

func (f feishuTestSetup) step(t *testing.T, count int) {
	t.Helper()
	for range count {
		ok, err := f.channel.ProcessOne(t.Context())
		if err != nil || !ok {
			t.Fatalf("unexpected worker result: processed=%t err=%v stage=%s", ok, err, f.jobs.job.Stage)
		}
	}
}

func TestFeishuGroupReceiptCannotDiscloseAnswer(t *testing.T) {
	allowed := setupFeishuTest(t, domain.FeishuReceipt, "ou_caller", "订单同步失败怎么办")
	channel := allowed.channel
	channel.appID, channel.tenantKey = feishuTestAppID, feishuTestTenant
	allowed.directory["ou_sender"] = receptionCaller
	event := feishuTestEvent("group", "@_user_1 @_user_2 订单同步失败怎么办",
		feishuTestMention("@_user_1", feishuTestBot), feishuTestMention("@_user_2", "ou_owner"))
	event.OccurredAt = channel.now().Format(time.RFC3339Nano)
	previous := allowed.jobs.job.ID
	for _, providerTime := range []string{"", "invalid", strconv.FormatInt(channel.now().Add(-48*time.Hour).UnixMilli(), 10)} {
		event.Payload["providerCreateTime"] = providerTime
		if err := channel.Enqueue(t.Context(), event); err != nil || allowed.jobs.job.ID != previous {
			t.Fatalf("replay using fresh receipt time was accepted: %q, %v", providerTime, err)
		}
	}
	event.Payload["providerCreateTime"] = strconv.FormatInt(channel.now().UnixMilli(), 10)
	if err := channel.Enqueue(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if allowed.jobs.job.Stage != domain.FeishuReceipt || allowed.jobs.job.CallerID != receptionCaller {
		t.Fatalf("wrong durable group job: %+v", allowed.jobs.job)
	}
	group := sentFeishuMessage{"chat_id", feishuTestChatID, feishuRequestID("receipt", allowed.jobs.job.ID), feishuReceiptText}
	allowed.step(t, 5)
	if len(allowed.sender.messages) != 2 || allowed.sender.messages[0] != group {
		t.Fatalf("expected fixed group receipt and separate private dispatch: %+v", allowed.sender.messages)
	}
	private := allowed.sender.messages[1]
	if private.idType != "open_id" || private.recipient != "ou_sender" || private.text != "共有排查步骤" {
		t.Fatalf("wrong authorized delivery: %+v", private)
	}

	for _, sender := range []string{"ou_other", "ou_unbound"} {
		t.Run(sender, func(t *testing.T) {
			denied := setupFeishuTest(t, domain.FeishuReceipt, sender, "订单同步失败怎么办")
			denied.step(t, 3)
			if len(denied.sender.messages) != 1 || denied.sender.messages[0].text != group.text || denied.sender.messages[0].idType != "chat_id" || len(denied.inquiries.inquiries) != 0 {
				t.Fatalf("group receipt changed or unauthorized inquiry was stored: %+v", denied.sender.messages)
			}
		})
	}
}

func TestFeishuPrivateRetryRequiresLiveBindingAndDisclosure(t *testing.T) {
	for _, reason := range []string{"unlinked", "audience_lost", "still_authorized"} {
		t.Run(reason, func(t *testing.T) {
			f := setupFeishuTest(t, domain.FeishuQuestion, "ou_caller", "订单同步失败怎么办")
			f.sender.retryable, f.sender.err = true, errors.New("synthetic connection timeout")
			f.step(t, 3)
			if len(f.sender.messages) != 1 || f.jobs.job.Stage != domain.FeishuAnswer {
				t.Fatalf("private send did not schedule a retry: %+v", f.jobs.job)
			}
			first := f.sender.messages[0]
			switch reason {
			case "unlinked":
				delete(f.directory, "ou_caller")
			case "audience_lost":
				f.ids[receptionCaller] = employee(receptionCaller)
			}
			f.sender.err = nil
			f.channel.now = func() time.Time { return f.jobs.job.NextAt }
			f.step(t, 1)
			if reason == "still_authorized" {
				if len(f.sender.messages) != 2 || f.sender.messages[1] != first || f.jobs.job.Outcome != "answer_sent" {
					t.Fatalf("retry diverged: %+v", f.sender.messages)
				}
			} else if len(f.sender.messages) != 1 {
				t.Fatalf("retry escaped disclosure check: %+v", f.sender.messages)
			}
		})
	}
}

func TestFeishuHumanReplyRemainsInHeyaIfPrivateDeliveryFails(t *testing.T) {
	f := setupFeishuTest(t, domain.FeishuQuestion, "ou_caller", "机房什么时候搬迁")
	f.step(t, 1)
	request := feishuInquiryRequest(f.jobs.job)
	item := f.inquiries.inquiries[request]
	if item.Status != domain.InquiryPending {
		t.Fatalf("question did not become an existing Heya pending inquiry: %+v", item)
	}
	// Both reply handlers persist the same replied state and caller visibility.
	item.Status, item.Answer = domain.InquiryReplied, "请查看通知"
	f.inquiries.inquiries[request] = item
	f.sender.err = errors.New("synthetic recipient is outside bot availability")
	f.step(t, 2)
	if f.jobs.job.Outcome != "answer_failed" || len(f.sender.messages) != 1 || f.sender.messages[0].recipient != "ou_caller" {
		t.Fatalf("private failure changed route: %+v / %+v", f.jobs.job, f.sender.messages)
	}
	view, err := f.reception.inquiryView(t.Context(), f.ids[receptionCaller], item)
	if err != nil || view.Answer != "请查看通知" {
		t.Fatalf("reply lost in Heya: %+v, %v", view, err)
	}
}

func TestFeishuLinkedInquiryUsesConfiguredLongerDeadline(t *testing.T) {
	f := setupFeishuTest(t, domain.FeishuQuestion, "ou_caller", "机房什么时候搬迁")
	f.settings.value.InquiryTimeoutHours = 240
	f.jobs.job.ExpiresAt = f.channel.now().Add(7 * 24 * time.Hour)
	f.step(t, 1)
	if want := f.channel.now().Add(240*time.Hour + feishuRetryWindow); f.jobs.job.Stage != domain.FeishuAnswer ||
		f.jobs.job.ExpiresAt.Before(want.Add(-time.Minute)) {
		t.Fatalf("long-lived Heya inquiry lost its private delivery route: %+v", f.jobs.job)
	}
}
