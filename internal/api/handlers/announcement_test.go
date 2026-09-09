package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainannouncement "github.com/opensoha/soha/internal/domain/announcement"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type receiptReader struct {
	AnnouncementReader
	announcementID string
	query          domainannouncement.ReceiptQuery
}

func (r *receiptReader) Receipts(_ context.Context, _ domainidentity.Principal, announcementID string, query domainannouncement.ReceiptQuery) (domainannouncement.ReceiptPage, error) {
	r.announcementID = announcementID
	r.query = query
	return domainannouncement.ReceiptPage{Items: []domainannouncement.Receipt{}, Total: 3, Page: query.Page, PageSize: query.PageSize, ReadCount: 1, UnreadCount: 2}, nil
}

func TestAnnouncementReceiptsPassesFiltersAndReturnsPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &receiptReader{}
	handler := NewAnnouncementHandlerWithServices(reader, nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/?keyword=Ada&state=unread&page=2&pageSize=15", nil)
	ctx.Params = gin.Params{{Key: "announcementID", Value: "announcement-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "admin-1"})

	handler.Receipts(ctx)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if reader.announcementID != "announcement-1" || reader.query.Keyword != "Ada" || reader.query.State != "unread" || reader.query.Page != 2 || reader.query.PageSize != 15 {
		t.Fatalf("query = %#v, announcementID = %q", reader.query, reader.announcementID)
	}
	if body := response.Body.String(); !strings.Contains(body, `"readCount":1`) || !strings.Contains(body, `"unreadCount":2`) {
		t.Fatalf("body = %s", body)
	}
}
