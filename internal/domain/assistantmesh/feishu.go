package assistantmesh

import "time"

const (
	FeishuReceipt  = "receipt"
	FeishuQuestion = "question"
	FeishuAnswer   = "answer"
	FeishuDone     = "done"
)

// FeishuJob is private channel state. It must not be exposed through the
// general event stream. Question text is discarded as soon as Heya takes it.
type FeishuJob struct {
	Scope, ID, Stage             string
	GroupID, SenderID, TargetID  string
	Question, CallerID, OwnerID  string
	InquiryID, ClaimID, Outcome  string
	Attempts                     int
	NextAt, StartedAt, ExpiresAt time.Time
}
