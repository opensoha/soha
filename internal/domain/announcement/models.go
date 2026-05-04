package announcement

import (
	"context"
	"time"
)

type Record struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Summary     string  `json:"summary"`
	Content     string  `json:"content"`
	Level       string  `json:"level"`
	Status      string  `json:"status"`
	Audience    string  `json:"audience"`
	Sticky      bool    `json:"sticky"`
	StartsAt    *string `json:"startsAt,omitempty"`
	EndsAt      *string `json:"endsAt,omitempty"`
	PublishedAt *string `json:"publishedAt,omitempty"`
	CreatedBy   string  `json:"createdBy,omitempty"`
	UpdatedBy   string  `json:"updatedBy,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type Input struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Summary  string  `json:"summary"`
	Content  string  `json:"content"`
	Level    string  `json:"level"`
	Status   string  `json:"status"`
	Audience string  `json:"audience"`
	Sticky   bool    `json:"sticky"`
	StartsAt *string `json:"startsAt,omitempty"`
	EndsAt   *string `json:"endsAt,omitempty"`
}

type InboxItem struct {
	Record
	IsRead bool    `json:"isRead"`
	ReadAt *string `json:"readAt,omitempty"`
}

type Inbox struct {
	Items       []InboxItem `json:"items"`
	UnreadCount int         `json:"unreadCount"`
}

type Repository interface {
	List(context.Context, int) ([]Record, error)
	Get(context.Context, string) (Record, error)
	Create(context.Context, Record) (Record, error)
	Update(context.Context, string, Record) (Record, error)
	Delete(context.Context, string) error
	Publish(context.Context, string, time.Time, string) (Record, error)
	Withdraw(context.Context, string, time.Time, string) (Record, error)
	ListInbox(context.Context, string, int, time.Time) (Inbox, error)
	MarkRead(context.Context, string, string, time.Time) error
}
