package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

// Return a mismatched payload ID to independently assert that even a stale
// adapter cannot redirect deletion away from the requested, authorized row.
type memoryDeleteStore struct {
	*appmemory.MemoryStore
	rows    map[string]appmemory.Record
	deleted []string
}

func (s *memoryDeleteStore) GetRecord(_ context.Context, id string) (appmemory.Record, error) {
	v, ok := s.rows[id]
	if !ok {
		return appmemory.Record{}, appmemory.ErrNotFound
	}
	return v, nil
}
func (s *memoryDeleteStore) DeleteRecord(_ context.Context, id string, _ time.Time) error {
	v, ok := s.rows[id]
	if !ok {
		return appmemory.ErrNotFound
	}
	v.Status = "deleted"
	s.rows[id] = v
	s.deleted = append(s.deleted, id)
	return nil
}

func TestMemoryDeleteBindsAuthorizedRequestedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, actor, rowID, payloadID string
		quarantined                   bool
		status                        int
	}{
		{"other_owner_cannot_delete_conflicting_record", "bob", "r", "target", true, http.StatusForbidden},
		{"owner_can_delete_quarantined_requested_row", "alice", "r", "target", true, http.StatusNoContent},
		{"normal_exact_record_delete", "alice", "normal", "normal", false, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memoryDeleteStore{MemoryStore: appmemory.NewMemoryStore(), rows: map[string]appmemory.Record{
				tc.rowID: {ID: tc.payloadID, OwnerType: "user", OwnerID: "alice", Status: "active", Quarantined: tc.quarantined},
				"target": {ID: "target", OwnerType: "user", OwnerID: "carol", Status: "active"},
			}}
			svc, err := appmemory.NewService(store)
			if err != nil {
				t.Fatal(err)
			}
			h, _, _, _ := newAdvancedHandlerForTest(t)
			h.memory = svc
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodDelete, "/ai/memory/"+tc.rowID, nil)
			c.Params = gin.Params{{Key: "memoryID", Value: tc.rowID}}
			c.Set("principal", domainidentity.Principal{UserID: tc.actor})
			h.deleteMemory(c)
			c.Writer.WriteHeaderNow()
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if store.rows["target"].Status != "active" {
				t.Fatal("deleted another row via payload identity")
			}
			if tc.status == http.StatusForbidden {
				if len(store.deleted) != 0 {
					t.Fatal("unauthorized delete", store.deleted)
				}
			} else if len(store.deleted) != 1 || store.deleted[0] != tc.rowID || store.rows[tc.rowID].Status != "deleted" {
				t.Fatal("wrong deletion target", store.deleted)
			}
		})
	}
}
