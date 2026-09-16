package deliverytrigger

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Start owns only durable trigger dispatch. Execution remains in the existing Batch/Sync services.
func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			s.schedule(ctx, time.Now().UTC())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			event, err := s.repo.Claim(ctx, uuid.NewString(), time.Now().UTC())
			if err == nil {
				s.process(ctx, event)
				continue
			}
			timer := time.NewTimer(3 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (s *Service) schedule(ctx context.Context, now time.Time) {
	for offset := 0; ; offset += 200 {
		items, err := s.repo.List(ctx, "", "", offset, 200)
		if err != nil {
			return
		}
		for _, item := range items {
			if !item.Enabled || item.Schedule == nil {
				continue
			}
			slot, due := scheduleSlot(*item.Schedule, now)
			if !due {
				continue
			}
			// Stable wall-clock identity survives restart, multi-server races and DST repetition.
			eventID := string(item.Type) + ":" + slot
			payloadDigest, err := digest(eventID)
			if err != nil {
				continue
			}
			_, err = s.repo.Enqueue(ctx, domain.StoredEvent{Event: domain.Event{ID: uuid.NewString(), TriggerID: item.ID, TriggerRevision: item.Revision, EventID: eventID, EventType: sohaapi.DeliveryTriggerEventEventType(item.Type), Status: "queued", OccurredAt: now.Truncate(time.Minute), CreatedAt: now, UpdatedAt: now}, PayloadDigest: payloadDigest})
			if err != nil && !errors.Is(err, apperrors.ErrConflict) {
				return
			}
		}
		if len(items) < 200 {
			return
		}
	}
}
