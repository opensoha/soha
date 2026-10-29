package endpointcontrol

import (
	"context"
	"encoding/json"
	"errors"
	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type reportStore struct{ calls int }

func (s *reportStore) View(context.Context, string) (ec.View, error) { return ec.View{}, nil }
func (s *reportStore) Save(context.Context, string, string, ec.SaveInput, time.Time) (ec.Policy, error) {
	return ec.Policy{}, nil
}
func (s *reportStore) Publish(context.Context, string, string, int64, time.Time) (ec.Policy, error) {
	return ec.Policy{}, nil
}
func (s *reportStore) Desired(context.Context, runtime.Credential, string) (ec.Desired, error) {
	return ec.Desired{}, nil
}
func (s *reportStore) Report(context.Context, runtime.Credential, ec.Receipt) error {
	s.calls++
	return nil
}
func TestReportRejectsAnotherDeviceBeforePersistence(t *testing.T) {
	s := &reportStore{}
	r, err := NewRuntime(s)
	if err != nil {
		t.Fatal(err)
	}
	in := ec.Receipt{SchemaVersion: ec.Version, DeviceID: "foreign", RuntimeID: "runtime", CredentialID: "credential", PolicyVersion: 1, Challenge: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObservedAt: time.Now().UTC(), Channels: []ec.Channel{{Name: "usb", Scope: "current_usb_volumes", State: "off", ReasonCode: "disabled"}, {Name: "print", Scope: "local_new_print_jobs", State: "off", ReasonCode: "disabled"}, {Name: "clipboard", Scope: "enrolled_console_win32", State: "off", ReasonCode: "disabled"}}}
	raw, _ := json.Marshal(in)
	err = r.Report(context.Background(), runtime.Credential{DeviceID: "own", RuntimeID: "runtime", ID: "credential"}, raw)
	if !errors.Is(err, apperrors.ErrInvalidArgument) || s.calls != 0 {
		t.Fatal("unbound report reached persistence", err)
	}
}
