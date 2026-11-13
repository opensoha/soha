package personalguest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/opensoha/soha-agent/internal/agent/config"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func (b *runtimeBroker) browserNetwork(ctx context.Context, op guest.RuntimeOperation, input guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error) {
	var result guest.BrowserNetworkResponse
	if input.Validate() != nil {
		return result, errors.New("browser request denied")
	}
	raw, err := config.ReadPrivateFile(b.cfg.StateFile, 16384)
	var current state
	if err != nil || decodeDataJSON(raw, &current) != nil || current.Binding == nil {
		return result, errors.New("current browser pin unavailable")
	}
	cert := current.ActiveCertificatePEM
	if current.RenewalResult != nil {
		cert = current.RenewalResult.CertificatePEM
	}
	client, err := guestHTTPClientWithCertificate(b.cfg, cert)
	if err != nil {
		return result, err
	}
	defer client.CloseIdleConnections()
	raw, err = json.Marshal(input)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.Endpoint+guest.BindingsPath+"/"+b.cfg.BindingID+"/browser-network/"+op.OperationID, bytes.NewReader(raw))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return result, errors.New("browser network unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil || len(raw) > 2<<20 || response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || decodeDataJSON(raw, &result) != nil || result.Validate() != nil {
		return result, errors.New("browser network authority denied")
	}
	return result, nil
}
