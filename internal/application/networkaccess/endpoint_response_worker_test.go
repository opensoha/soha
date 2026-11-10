package networkaccess

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

type blockedNotificationStore struct {
	EndpointResponseStore
	nasChecked, notificationStarted chan struct{}
}

func (s *blockedNotificationStore) EndpointResponseCandidates(context.Context, string, int) ([]domain.EndpointResponseCandidate, error) {
	return nil, nil
}

func (s *blockedNotificationStore) NASAdmissionCandidates(context.Context, string, int) ([]string, error) {
	close(s.nasChecked)
	return nil, nil
}

func (s *blockedNotificationStore) ClaimEndpointNotification(ctx context.Context, _ time.Time) (domain.EndpointResponseRecord, error) {
	close(s.notificationStarted)
	<-ctx.Done()
	return domain.EndpointResponseRecord{}, ctx.Err()
}

func TestSlowNotificationDoesNotDelayAdmissionRevalidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &blockedNotificationStore{nasChecked: make(chan struct{}), notificationStarted: make(chan struct{})}
		service := &EndpointResponseService{store: store, now: time.Now}
		report := func(err error) { t.Errorf("worker failed: %v", err) }
		go service.Run(ctx, report)
		go service.RunNotifications(ctx, report)
		time.Sleep(11 * time.Second)
		synctest.Wait()
		for name, signal := range map[string]<-chan struct{}{"NAS revalidation": store.nasChecked, "blocked notification": store.notificationStarted} {
			select {
			case <-signal:
			default:
				t.Fatalf("%s did not run independently", name)
			}
		}
		cancel()
		synctest.Wait()
	})
}
