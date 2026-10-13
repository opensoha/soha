package copilot

import (
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"time"
)

type MessageSearchHit = sohaapi.WorkbenchMessageSearchHit
type MessageSearchPage = sohaapi.WorkbenchMessageSearchPage
type MessageCursor struct {
	Personal  bool
	CreatedAt time.Time
	ID        string
}
type MessageCandidate struct {
	Message      Message
	SessionTitle string
}
type MessageWindowInput struct {
	Around, Before, After string
	Limit                 int
}
type MessageWindow struct {
	Messages     []Message `json:"messages"`
	BeforeCursor string    `json:"beforeCursor,omitempty"`
	AfterCursor  string    `json:"afterCursor,omitempty"`
}
