package networkproxyruntime

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type IngestClient struct {
	url       string
	runtimeID string
	http      *http.Client
}

func NewIngestClient(origin, runtimeID string, tlsConfig *tls.Config) *IngestClient {
	transport := cloneDefaultTransport()
	transport.TLSClientConfig = tlsConfig
	return &IngestClient{url: strings.TrimRight(origin, "/") + "/api/ingest/v1/events:batch", runtimeID: runtimeID,
		http: &http.Client{Transport: transport, Timeout: 12 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *IngestClient) Send(ctx context.Context, observation domain.Observation) error {
	now := time.Now().UTC()
	payload, err := json.Marshal(networkprotocol.ProxyRuntimeSample{
		Engine: observation.Engine, UptimeSeconds: observation.UptimeSeconds,
		UploadTotal: observation.UploadTotal, DownloadTotal: observation.DownloadTotal,
		ActiveConnections: observation.ActiveConnections,
	})
	if err != nil {
		return err
	}
	id := uuid.NewString()
	batch, err := json.Marshal(networkprotocol.IngestBatch{
		SchemaVersion: networkprotocol.IngestSchemaVersion, BatchID: id,
		ProducerID: c.runtimeID, ProducerKind: "proxy", SentAt: now,
		Events: []networkprotocol.IngestEvent{{ID: id, Type: networkprotocol.EventProxyRuntimeSample,
			Sequence: now.UnixNano(), OccurredAt: now, Payload: payload}},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(batch))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("submit proxy sample: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("submit proxy sample: ingest returned HTTP %d", response.StatusCode)
	}
	return nil
}
