package contenttransfer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"testing"

	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
)

func managedArchive(t *testing.T, body, comment string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	if err := w.SetComment(comment); err != nil {
		t.Fatal(err)
	}
	f, err := w.Create("data.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestManagedArchiveGatesAndOriginalByteBinding(t *testing.T) {
	archive := managedArchive(t, "password=synthetic_archive_secret", "first")
	policy := d.Policy{Mode: "block", DetectorVersion: dlp.ArchiveDetectorVersion, Detectors: []string{"credential_assignment"}, Rules: []dlp.Rule{}, RequireStepUp: true}
	dest := d.Destination{ID: "destination", Published: &d.Config{Policy: policy}, PublishedVersion: 7}
	op := operation{id: "request", action: "upload", destinationID: dest.ID, name: "data.zip", key: "object", content: archive, gates: []scopeGate{{destination: dest}}}
	decision, _ := evaluate(context.Background(), op)
	if decision.Allowed || decision.Status != "matched" || decision.RequiredAction != "appeal" || !decision.StepUpRequired || decision.DetectorVersion != dlp.ArchiveDetectorVersion {
		t.Fatalf("decision=%+v", decision)
	}
	unknown := op
	unknown.content = managedArchive(t, "%PDF-1.7", "first")
	for _, mode := range []string{"warn", "block"} {
		unknown.gates[0].destination.Published.Policy.Mode = mode
		got, _ := evaluate(context.Background(), unknown)
		if got.Allowed || got.Status != "unsupported" || got.RequiredAction != "" || got.StepUpRequired {
			t.Fatalf("uninspected release offered: %+v", got)
		}
	}
	legacy := d.Destination{Published: &d.Config{Policy: d.Policy{Mode: "block", Detectors: []string{"private_key"}, Rules: []dlp.Rule{}}}}
	op.gates = append(op.gates, scopeGate{destination: legacy})
	got, _ := evaluate(context.Background(), op)
	if got.Allowed || got.Status != "unsupported" || got.RequiredAction != "" || got.StepUpRequired {
		t.Fatalf("legacy source gate bypassed: %+v", got)
	}
	testAuditedArchiveEnforcement(t, op, dest, legacy)
	actor := identity.Principal{UserID: "owner"}
	key := []byte("synthetic-archive-binding-key-000")
	other := op
	other.content = managedArchive(t, "password=synthetic_archive_secret", "second")
	gates := []d.Gate{{DestinationID: dest.ID, PolicyVersion: 7}}
	if hmac.Equal(operationTag(key, actor, op, gates, []string{"backend"}), operationTag(key, actor, other, gates, []string{"backend"})) || hmac.Equal(requestTag(key, actor, op), requestTag(key, actor, other)) {
		t.Fatal("equivalent extracted text replaced the approved original archive")
	}
}

func testAuditedArchiveEnforcement(t *testing.T, op operation, dest, legacy d.Destination) {
	t.Helper()
	legacy.Published.Policy.Mode = "audit"
	for _, mode := range []string{"warn", "block"} {
		dest.Published.Policy.Mode = mode
		dest.Published.Policy.NetworkAction = "none"
		action := "acknowledge"
		if mode == "block" {
			action = "appeal"
			dest.Published.Policy.NetworkAction = "revoke_network"
		}
		for _, gates := range [][]scopeGate{{{destination: legacy}, {destination: dest}}, {{destination: dest}, {destination: legacy}}} {
			op.gates = gates
			decision, _, network := evaluateWithLink(context.Background(), op)
			if decision.Allowed || decision.Status != "matched" || decision.RequiredAction != action || !decision.StepUpRequired || network != (mode == "block") {
				t.Fatalf("audit erased enforcement: %+v network=%v", decision, network)
			}
		}
	}
}
