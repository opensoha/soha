package copilot

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestLegacyMessageProjectionPreservesEmptyArrayContract(t *testing.T) {
	for _, messages := range [][]domaincopilot.Message{nil, {}} {
		var unavailable *Service
		for _, result := range [][]domaincopilot.Message{markLegacyPlatformContextMessages(messages), unavailable.migrateLegacyPlatformContextMessages(t.Context(), "session", messages)} {
			raw, err := json.Marshal(map[string]any{"items": result})
			if err != nil || !bytes.Equal(raw, []byte(`{"items":[]}`)) {
				t.Fatalf("required public array became null: %s, error=%v", raw, err)
			}
		}
	}
}

func TestSessionMetadataArchiveTimestampMatchesPublicDTO(t *testing.T) {
	for _, archived := range []string{"", "2026-10-25T04:00:00Z"} {
		t.Run(archived, func(t *testing.T) {
			metadata := sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general", ArchivedAt: archived})
			if archived == "" {
				if _, present := metadata["archivedAt"]; present {
					t.Fatal("unset archive time must be omitted")
				}
			} else if metadata["archivedAt"] != archived {
				t.Fatal("set archive time was changed")
			}
			assertSessionPublicDTO(t, domaincopilot.Session{ID: "session-1", Metadata: metadata}, archived)
		})
	}
}

func TestVisibleSessionNormalizesLegacyEmptyArchiveWithoutMutatingStorage(t *testing.T) {
	stored := map[string]any{"mode": "general", "archivedAt": "", "privateUnrelated": "preserved"}
	visible := (&Service{}).visibleSessionSummary(t.Context(), domainidentity.Principal{}, domaincopilot.Session{ID: "session-1", Metadata: stored})
	if stored["archivedAt"] != "" || visible.Metadata["privateUnrelated"] != "preserved" {
		t.Fatal("projection mutated stored state or lost another field")
	}
	if _, present := visible.Metadata["archivedAt"]; present {
		t.Fatal("legacy empty timestamp leaked into the public DTO")
	}
	assertSessionPublicDTO(t, visible, "")
}

func assertSessionPublicDTO(t *testing.T, session domaincopilot.Session, archived string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"data": session})
	if err != nil {
		t.Fatal(err)
	}
	var envelope sohaapi.WorkbenchSessionEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("public generated DTO cannot read session: %v", err)
	}
	if envelope.Data.Metadata == nil {
		t.Fatal("session metadata was lost")
	}
	if archived == "" {
		if envelope.Data.Metadata.ArchivedAt != nil {
			t.Fatal("unset archive time became a date")
		}
		return
	}
	expected, err := time.Parse(time.RFC3339, archived)
	if err != nil || envelope.Data.Metadata.ArchivedAt == nil || !envelope.Data.Metadata.ArchivedAt.Equal(expected) {
		t.Fatal("archive date did not round trip")
	}
}
