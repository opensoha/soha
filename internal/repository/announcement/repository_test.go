package announcement

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domainannouncement "github.com/opensoha/soha/internal/domain/announcement"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestListReceiptsReturnsFilteredUsersAndOverallCounts(t *testing.T) {
	repository, mock := newAnnouncementRepository(t)
	mock.ExpectQuery(`(?s)SELECT COUNT\(ar\.read_at\).*FROM users u.*LEFT JOIN announcement_receipts ar.*u\.status = 'active'.*ILIKE`).
		WithArgs("announcement-1", "Ada", "%Ada%", "%Ada%", "%Ada%").
		WillReturnRows(sqlmock.NewRows([]string{"read_count", "unread_count"}).AddRow(1, 2))
	mock.ExpectQuery(`(?s)SELECT u\.id, u\.username, COALESCE\(u\.display_name, ''\), u\.email.*json_agg\(t\.name ORDER BY t\.name\).*FROM users u.*LEFT JOIN announcement_receipts ar.*u\.status = 'active'.*ILIKE.*ar\.read_at IS NULL.*ORDER BY.*LIMIT`).
		WithArgs("announcement-1", "Ada", "%Ada%", "%Ada%", "%Ada%", "unread", "unread", "unread", 15, 15).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "display_name", "email", "team_names", "read_at"}).
			AddRow("5b550b5a-f8c8-47a7-ab42-57c030d30c3e", "ada", "Ada", "ada@example.test", []byte(`["研发中心"]`), nil))

	page, err := repository.ListReceipts(context.Background(), "announcement-1", domainannouncement.ReceiptQuery{
		Keyword:  "Ada",
		State:    "unread",
		Page:     2,
		PageSize: 15,
	})
	if err != nil {
		t.Fatalf("ListReceipts() error = %v", err)
	}
	if page.Total != 2 || page.ReadCount != 1 || page.UnreadCount != 2 || page.Page != 2 || page.PageSize != 15 {
		t.Fatalf("page = %#v", page)
	}
	if len(page.Items) != 1 || page.Items[0].Username != "ada" || len(page.Items[0].TeamNames) != 1 || page.Items[0].TeamNames[0] != "研发中心" || page.Items[0].IsRead || page.Items[0].ReadAt != nil {
		t.Fatalf("items = %#v", page.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newAnnouncementRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}
