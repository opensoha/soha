package contenttransfer

import (
	"context"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"testing"
	"time"
)

func TestManagedNetworkOnlyForEnforcedMatchedGate(t *testing.T) {
	for _, tt := range []struct {
		name, mode, body, min string
		want                  bool
	}{{"block match", "block", "-----BEGIN PRIVATE KEY-----", "public", true}, {"audit match", "audit", "-----BEGIN PRIVATE KEY-----", "public", false}, {"unknown block", "block", "\x00", "public", false}, {"clean block", "block", "ordinary", "public", false}} {
		t.Run(tt.name, func(t *testing.T) {
			op := operation{name: "file.txt", content: []byte(tt.body), gates: []scopeGate{{destination: d.Destination{Published: &d.Config{Policy: d.Policy{Mode: tt.mode, NetworkAction: "revoke_network", MinimumDataLevel: tt.min, Detectors: []string{"private_key"}, Rules: []dlp.Rule{}}}}}}}
			_, _, got := evaluateWithLink(context.Background(), op)
			if got != tt.want {
				t.Fatalf("network=%v", got)
			}
		})
	}
}
func TestNetworkReviewDoesNotExtendTransferExpiry(t *testing.T) {
	r := d.Record{Revision: 4, State: "blocked", AppealStatus: "pending", Decision: &dlp.Decision{Status: "matched", RequiredAction: "appeal"}, Private: d.Private{ExpiresAt: time.Now().Add(-time.Hour)}}
	in := d.ReviewInput{ExpectedRevision: 4, Resolution: "approve"}
	if reviewable(r, in) {
		t.Fatal("expired unlinked appeal accepted")
	}
	r.NetworkHoldID = "hold"
	if !reviewable(r, in) {
		t.Fatal("network hold cannot be reviewed after transfer expiry")
	}
	r.AppealStatus = "rejected"
	if !reviewable(r, in) {
		t.Fatal("independent reconsideration cannot approve a rejected hold")
	}
	in.Resolution = "reject"
	if reviewable(r, in) {
		t.Fatal("unnecessary rejected-to-rejected write")
	}
}

func TestCompositeEvidenceKeepsContributingThreshold(t *testing.T) {
	result := dlp.Decision{Mode: "block", Status: "matched", Allowed: false, DataLevel: "restricted", MinimumDataLevel: "restricted"}
	mergeDecision(&result, dlp.Decision{Mode: "warn", Status: "matched", Allowed: false, DataLevel: "confidential", MinimumDataLevel: "confidential"})
	if result.Mode != "block" || result.MinimumDataLevel != "restricted" {
		t.Fatalf("threshold no longer belongs to the blocking policy: %+v", result)
	}
}
