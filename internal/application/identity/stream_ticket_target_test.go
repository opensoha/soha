package identity

import (
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	"testing"
	"time"
)

func TestStreamTicketPinsPodScope(t *testing.T) {
	path := "/api/v1/clusters/c/workloads/pods/api/terminal"
	saved := normalizeStreamTicketPath(path + "?expectedUid=uid-a&namespace=test&container=main&shell=%2Fbin%2Fsh")
	same := normalizeStreamTicketPath(path + "?container=main&shell=%2Fbin%2Fsh&namespace=test&expectedUid=uid-a&stream_ticket=credential")
	if saved != same || !isAllowedStreamTicketPath(saved) {
		t.Fatalf("target mismatch: %q %q", saved, same)
	}
	for _, query := range []string{"expectedUid=uid-b&namespace=test&container=main&shell=%2Fbin%2Fsh", "expectedUid=uid-a&namespace=prod&container=main&shell=%2Fbin%2Fsh", "expectedUid=uid-a&namespace=test&container=sidecar&shell=%2Fbin%2Fsh"} {
		if normalizeStreamTicketPath(path+"?"+query) == saved {
			t.Fatal("changed target accepted")
		}
	}
	if got := normalizeStreamTicketPath(path + "?expectedUid=&expectedUid=b"); got != "" {
		t.Fatalf("duplicate UID accepted: %q", got)
	}
	if got := streamTicketRequestTarget(path, same); got != path {
		t.Fatalf("legacy target = %q", got)
	}
}

func TestPodTicketRejectsChangedUIDAndKeepsSingleUse(t *testing.T) {
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{ID: "u1", Username: "user1", Status: "active", AuthzVersion: 1}
	repo.sessionsByID["s1"] = userrepo.Session{ID: "s1", UserID: "u1", Status: "active", ExpiresAt: time.Now().Add(time.Hour), AuthzVersion: 1}
	service := newTestServiceWithUserStore(repo)
	path := "/api/v1/clusters/c/workloads/pods/api/terminal?namespace=test&container=main&expectedUid=original"
	for _, query := range []string{"original", "replacement"} {
		ticket, err := service.IssueStreamTicket(t.Context(), domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = service.ParseStreamTicket(t.Context(), ticket.Ticket, "/api/v1/clusters/c/workloads/pods/api/terminal?container=main&namespace=test&expectedUid="+query)
		if (err == nil) != (query == "original") {
			t.Fatalf("UID %s: %v", query, err)
		}
		if _, _, err := service.ParseStreamTicket(t.Context(), ticket.Ticket, path); err == nil {
			t.Fatal("ticket was reusable")
		}
	}
}
