package announcement

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainannouncement "github.com/opensoha/soha/internal/domain/announcement"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type announcementRolePermissions struct {
	permissions map[string][]string
}

func (r announcementRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.permissions, nil
}

func TestAuthorizeInboxAcceptsPortalViewPermission(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(announcementRolePermissions{
		permissions: map[string][]string{
			"portal-user": {appaccess.PermIdentityPortalView},
		},
	})
	service := &Service{permissions: resolver}

	if err := service.authorizeInbox(context.Background(), domainidentity.Principal{Roles: []string{"portal-user"}}); err != nil {
		t.Fatalf("authorizeInbox() error = %v", err)
	}
}

func TestAuthorizeInboxRejectsPrincipalWithoutReadPermission(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(announcementRolePermissions{
		permissions: map[string][]string{
			"viewer": {appaccess.PermIdentityApplicationsView},
		},
	})
	service := &Service{permissions: resolver}

	if err := service.authorizeInbox(context.Background(), domainidentity.Principal{Roles: []string{"viewer"}}); err == nil {
		t.Fatal("authorizeInbox() error = nil, want access denied")
	}
}

type announcementReceiptRepository struct {
	domainannouncement.Repository
	query          domainannouncement.ReceiptQuery
	announcementID string
	listCalls      int
}

func (r *announcementReceiptRepository) Get(_ context.Context, id string) (domainannouncement.Record, error) {
	return domainannouncement.Record{ID: id, Title: "Maintenance"}, nil
}

func (r *announcementReceiptRepository) ListReceipts(_ context.Context, announcementID string, query domainannouncement.ReceiptQuery) (domainannouncement.ReceiptPage, error) {
	r.announcementID = announcementID
	r.query = query
	r.listCalls++
	return domainannouncement.ReceiptPage{Page: query.Page, PageSize: query.PageSize}, nil
}

func TestReceiptsRequiresSystemViewAndNormalizesQuery(t *testing.T) {
	repo := &announcementReceiptRepository{}
	resolver := appaccess.NewPermissionResolver(announcementRolePermissions{
		permissions: map[string][]string{
			"admin":       {appaccess.PermSystemAnnouncementsView},
			"portal-user": {appaccess.PermIdentityPortalView},
		},
	})
	service := New(repo, resolver, nil, nil)

	page, err := service.Receipts(
		context.Background(),
		domainidentity.Principal{Roles: []string{"admin"}},
		" announcement-1 ",
		domainannouncement.ReceiptQuery{Keyword: " Ada ", State: " READ ", PageSize: 500},
	)
	if err != nil {
		t.Fatalf("Receipts() error = %v", err)
	}
	if repo.announcementID != "announcement-1" || repo.query.Keyword != "Ada" || repo.query.State != "read" || repo.query.Page != 1 || repo.query.PageSize != 100 {
		t.Fatalf("normalized receipt query = %#v, announcementID = %q", repo.query, repo.announcementID)
	}
	if page.Page != 1 || page.PageSize != 100 {
		t.Fatalf("page = %#v", page)
	}

	_, err = service.Receipts(
		context.Background(),
		domainidentity.Principal{Roles: []string{"portal-user"}},
		"announcement-1",
		domainannouncement.ReceiptQuery{},
	)
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Receipts() error = %v, want access denied", err)
	}
	if repo.listCalls != 1 {
		t.Fatalf("ListReceipts() calls = %d, want 1", repo.listCalls)
	}
}

func TestReceiptsRejectsInvalidState(t *testing.T) {
	repo := &announcementReceiptRepository{}
	resolver := appaccess.NewPermissionResolver(announcementRolePermissions{
		permissions: map[string][]string{"admin": {appaccess.PermSystemAnnouncementsView}},
	})
	service := New(repo, resolver, nil, nil)

	_, err := service.Receipts(
		context.Background(),
		domainidentity.Principal{Roles: []string{"admin"}},
		"announcement-1",
		domainannouncement.ReceiptQuery{State: "maybe"},
	)
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Receipts() error = %v, want invalid argument", err)
	}
	if repo.listCalls != 0 {
		t.Fatalf("ListReceipts() calls = %d, want 0", repo.listCalls)
	}
}
