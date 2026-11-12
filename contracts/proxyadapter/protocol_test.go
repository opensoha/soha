package proxyadapter

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

func TestAdapterResultFixtures(t *testing.T) {
	now := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	for _, method := range []Method{Handshake, Apply, Observe, Close} {
		for _, valid := range []bool{true, false} {
			dir, name := "valid", string(method)+"-result.json"
			if !valid {
				dir, name = "invalid", string(method)+"-incomplete-result.json"
			}
			t.Run(dir+"/"+name, func(t *testing.T) {
				raw, err := fs.ReadFile(os.DirFS("../fixtures/json-schema/proxy-adapter"), path.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				var line bytes.Buffer
				if err := json.Compact(&line, raw); err != nil {
					t.Fatal(err)
				}
				line.WriteByte('\n')
				if _, err = DecodeFrame(line.Bytes(), now); (err == nil) != valid {
					t.Fatalf("DecodeFrame error = %v, want valid = %v", err, valid)
				}
			})
		}
	}
}

func TestFrameBoundariesAndCorrelation(t *testing.T) {
	now := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	f := Frame{Major: 1, Minor: 0, RequestID: "r1", ProcessEpoch: "e1", Deadline: now.Add(time.Second), Method: Handshake, Payload: &Payload{Challenge: strings.Repeat("a", 32)}}
	raw, _ := json.Marshal(f)
	raw = append(raw, '\n')
	if _, err := DecodeFrame(raw, now); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{raw[:len(raw)-1], append(append([]byte{}, raw...), raw...), []byte(strings.Repeat("x", MaxFrameBytes+1))} {
		if _, err := DecodeFrame(bad, now); err == nil {
			t.Fatal("invalid boundary accepted")
		}
	}
	f.Major = 2
	if err := f.Validate(now); err == nil {
		t.Fatal("major accepted")
	}
	f.Major = 1
	f.Deadline = now
	if err := f.Validate(now); err == nil {
		t.Fatal("expiry accepted")
	}
}
func TestCloseWindowAndEpoch(t *testing.T) {
	now := time.Now()
	if ValidateWindow(now, now.Add(31*time.Second), now) == nil {
		t.Fatal("excessive lifetime")
	}
	req := Frame{Major: 1, RequestID: "r1", ProcessEpoch: "e1", Deadline: now.Add(time.Second), Method: Stop, Payload: &Payload{}}
	reply := req
	reply.Payload = nil
	reply.Result = &Result{}
	reply.ProcessEpoch = "e2"
	if MatchResponse(req, reply, "", now) == nil {
		t.Fatal("cross epoch accepted")
	}
}
