package networkproxyruntime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxyadapter"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

// ServeReferenceAdapter is an explicitly selected stdio reference executable.
// It owns no data plane: the host starts and independently verifies the pinned
// Mihomo-compatible engine. Installers may pin a wrapper invoking this mode.
func ServeReferenceAdapter(input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, proxyadapter.MaxFrameBytes+1)
	state := referenceAdapterState{}
	for {
		line, err := reader.ReadSlice('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		frame, err := proxyadapter.DecodeFrame(line, time.Now())
		if err != nil {
			return err
		}
		if frame.Payload == nil {
			return fmt.Errorf("request payload required")
		}
		if state.epoch != "" && state.epoch != frame.ProcessEpoch {
			return fmt.Errorf("epoch mismatch")
		}
		state.epoch = frame.ProcessEpoch
		response := frame
		response.Payload = nil
		ctx, cancel := context.WithDeadline(context.Background(), frame.Deadline)
		response.Result, err = state.handle(ctx, frame)
		cancel()
		if err != nil {
			return err
		}
		if response.Result == nil {
			response.Error = &proxyadapter.ProtocolError{Code: "unsupported", Message: "operation unsupported"}
		}
		raw, err := proxyadapter.EncodeFrame(response, time.Now())
		if err != nil {
			return err
		}
		if _, err := output.Write(raw); err != nil {
			return err
		}
		if frame.Method == proxyadapter.Stop {
			return nil
		}
	}
}

type referenceAdapterState struct {
	prepared   *proxyadapter.Payload
	controller *clashController
	sequence   int64
	epoch      string
}

func (s *referenceAdapterState) handle(ctx context.Context, frame proxyadapter.Frame) (*proxyadapter.Result, error) {
	switch frame.Method {
	case proxyadapter.Handshake:
		return &proxyadapter.Result{Challenge: frame.Payload.Challenge, Descriptor: &api.NetworkProxyEngineDescriptor{EngineID: "reference-mihomo", AdapterID: "soha-reference", Version: "1.0.0", ProtocolMajor: 1, Capabilities: []string{"traffic", "connections", "close_connection"}}}, nil
	case proxyadapter.Prepare:
		return s.prepare(frame.Payload)
	case proxyadapter.Apply:
		return s.apply(ctx, frame.Payload)
	case proxyadapter.Observe:
		return s.observe(ctx)
	case proxyadapter.Close:
		if s.controller == nil {
			return nil, fmt.Errorf("prepare required")
		}
		status, err := s.controller.close(ctx, frame.Payload.ConnectionID)
		if err != nil {
			status = "failed"
		}
		return &proxyadapter.Result{CloseStatus: status}, nil
	case proxyadapter.Stop:
		return &proxyadapter.Result{}, nil
	default:
		return nil, nil
	}
}
func (s *referenceAdapterState) prepare(p *proxyadapter.Payload) (*proxyadapter.Result, error) {
	if fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(p.Content))) != p.ContentHash {
		return nil, fmt.Errorf("content hash mismatch")
	}
	doc, err := decodeEngineDocument(domain.EngineMihomo, []byte(p.Content))
	if err != nil {
		return nil, err
	}
	address, ok := doc["external-controller"].(string)
	secret, secretOK := doc["secret"].(string)
	if !ok || !secretOK || loopbackHTTP("http://"+address) != nil {
		return nil, fmt.Errorf("managed controller required")
	}
	s.controller = newClashController("http://"+address, secret)
	s.prepared = p
	return &proxyadapter.Result{}, nil
}
func (s *referenceAdapterState) apply(ctx context.Context, p *proxyadapter.Payload) (*proxyadapter.Result, error) {
	if s.controller == nil || s.prepared == nil || s.prepared.ContentHash != p.ContentHash {
		return nil, fmt.Errorf("prepare required")
	}
	if _, err := s.controller.version(ctx); err != nil {
		return nil, err
	}
	return &proxyadapter.Result{Revision: s.prepared.Revision, ReadbackHash: s.prepared.ContentHash}, nil
}
func (s *referenceAdapterState) observe(ctx context.Context) (*proxyadapter.Result, error) {
	if s.controller == nil {
		return nil, fmt.Errorf("prepare required")
	}
	upload, download, active, connections, err := s.controller.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	s.sequence++
	o := &proxyadapter.Observation{SampleSequence: s.sequence, UploadTotal: upload, DownloadTotal: download, TotalActive: int64(active), ReturnedCount: int64(len(connections)), Truncated: active > len(connections), Freshness: "fresh", Connections: []api.NetworkProxyConnection{}}
	for _, c := range connections {
		o.Connections = append(o.Connections, api.NetworkProxyConnection{ID: c.ID, Destination: c.Destination, Network: api.NetworkProxyConnectionNetwork(c.Network), UploadBytes: c.UploadBytes, DownloadBytes: c.DownloadBytes, StartedAt: c.StartedAt})
	}
	return &proxyadapter.Result{Observation: o}, nil
}
