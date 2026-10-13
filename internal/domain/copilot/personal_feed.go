package copilot

import (
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"time"
)

type FeedSettings = sohaapi.PersonalFeedSettings
type FeedItem = sohaapi.PersonalFeedItem
type FeedPage = sohaapi.PersonalFeedPage
type FeedSource = sohaapi.PersonalFeedSource

const DefaultFeedInstructions = "根据我最近讨论的兴趣整理动态。内容简洁直接，便于快速浏览，提供可核实的来源，避免点击诱饵。"

type FeedRecord struct {
	Owner         string
	Settings      FeedSettings
	State         string
	Error         string
	JobID         string
	LastUpdatedAt *time.Time
}
