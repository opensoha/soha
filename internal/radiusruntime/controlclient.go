package radiusruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
)

type ClientConfiguration struct {
	AllowedConfigurations []api.NetworkRADIUSConfiguration `json:"-"`
	ApprovedMaterials     map[string]string                `json:"-"`
	URL                   string                           `json:"url"`
	RuntimeID             string                           `json:"runtimeId"`
	MemberID              string                           `json:"memberId"`
	PoolID                string                           `json:"poolId"`
	CertificateFile       string                           `json:"certificateFile"`
	KeyFile               string                           `json:"keyFile"`
	CAFile                string                           `json:"caFile"`
	ServerName            string                           `json:"serverName"`
	Configuration         api.NetworkRADIUSConfiguration   `json:"configuration"`
}
type ControlClient struct {
	config           ClientConfiguration
	client           *http.Client
	certificateUntil time.Time
	mu               sync.Mutex
	current          api.NetworkRADIUSRuntimeControl
}

func NewControlClient(config ClientConfiguration) (*ControlClient, error) {
	endpoint, err := url.Parse(config.URL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return nil, ErrInvalid
	}
	for _, id := range []string{config.RuntimeID, config.MemberID, config.PoolID} {
		if !identifier.MatchString(id) {
			return nil, ErrInvalid
		}
	}
	if err := verifyControlFiles(config, nativePaths()); err != nil {
		return nil, err
	}
	tlsConfig, err := networkidentity.LoadClientTLS(config.CertificateFile, config.KeyFile, config.CAFile, config.ServerName)
	if err != nil {
		return nil, fmt.Errorf("RADIUS control TLS configuration rejected")
	}
	if len(tlsConfig.Certificates) != 1 || len(tlsConfig.Certificates[0].Certificate) == 0 {
		return nil, ErrInvalid
	}
	leaf, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if err != nil || !leaf.NotAfter.After(time.Now()) || leaf.NotBefore.After(time.Now()) {
		return nil, ErrDenied
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, ErrInvalid
	}
	transport := base.Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = tlsConfig
	transport.MaxResponseHeaderBytes = 8192
	return &ControlClient{config: config, certificateUntil: leaf.NotAfter, client: &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func verifyControlFiles(config ClientConfiguration, paths pathPolicy) error {
	for _, path := range []string{config.CertificateFile, config.KeyFile, config.CAFile} {
		if _, err := paths.regular(path); err != nil {
			return ErrDenied
		}
		expected, ok := config.ApprovedMaterials[path]
		actual, err := hashFile(path)
		if !ok || !digestPattern.MatchString(expected) || err != nil || actual != expected {
			return ErrDenied
		}
	}
	return nil
}
func (c *ControlClient) Close() { c.client.CloseIdleConnections() }
func (c *ControlClient) endpoint(suffix string) string {
	return strings.TrimRight(c.config.URL, "/") + "/api/network-control/v1/radius-runtimes/" + url.PathEscape(c.config.RuntimeID) + suffix
}
func (c *ControlClient) request(ctx context.Context, method, suffix string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return ErrInvalid
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(suffix), body)
	if err != nil {
		return ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("RADIUS control request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return fmt.Errorf("RADIUS control HTTP %d", response.StatusCode)
	}
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" {
		return ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(output); err != nil {
		return ErrInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (c *ControlClient) validate(control api.NetworkRADIUSRuntimeControl, epoch string) error {
	if control.PoolID != c.config.PoolID || control.MemberID != c.config.MemberID || control.RuntimeID != c.config.RuntimeID || control.ProcessEpoch != epoch || !c.approvedConfiguration(control.Configuration) {
		return ErrDenied
	}
	if err := validateControlValues(control); err != nil {
		return err
	}
	stopOnly := control.StopRequired && control.StopConfirmUntil != nil && control.StopConfirmUntil.After(time.Now())
	if !control.Configuration.HooksAttested || !control.ControlLeaseUntil.After(time.Now()) || !stopOnly && (!control.ServeExistingUntil.After(time.Now()) || !control.ConfigurationLeaseUntil.After(time.Now())) {
		return ErrDenied
	}
	return nil
}
func validateControlValues(control api.NetworkRADIUSRuntimeControl) error {
	for _, id := range []string{control.PoolID, control.MemberID, control.RuntimeID, control.CredentialID, control.NASBindingID, control.NASID, control.SiteID, control.ProcessEpoch} {
		if !identifier.MatchString(id) {
			return ErrInvalid
		}
	}
	for _, revision := range []int{control.OwnerEpoch, control.ControlRevision, control.ConfigurationRevision} {
		if revision < 1 || int64(revision) > 9007199254740991 {
			return ErrInvalid
		}
	}
	for _, hash := range []string{control.ConfigurationHash, control.Configuration.TreeHash} {
		if !strings.HasPrefix(hash, "sha256:") || !digestPattern.MatchString(strings.TrimPrefix(hash, "sha256:")) {
			return ErrInvalid
		}
	}
	return nil
}

func controlBinding(control api.NetworkRADIUSRuntimeControl) string {
	encoded, _ := json.Marshal([]any{control.PoolID, control.MemberID, control.RuntimeID, control.CredentialID, control.OwnerEpoch, control.ConfigurationRevision, control.ConfigurationHash, control.Configuration})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func runtimeProof(control api.NetworkRADIUSRuntimeControl) string {
	encoded, _ := json.Marshal(map[string]any{"runtimeProof": map[string]any{"poolId": control.PoolID, "memberId": control.MemberID, "credentialId": control.CredentialID, "processEpoch": control.ProcessEpoch, "ownerEpoch": control.OwnerEpoch, "controlRevision": control.ControlRevision, "configurationRevision": control.ConfigurationRevision, "configurationHash": control.ConfigurationHash}})
	return "," + string(encoded[1:len(encoded)-1])
}
func (c *ControlClient) Fetch(ctx context.Context, epoch string) (Lease, error) {
	var control api.NetworkRADIUSRuntimeControl
	if !identifier.MatchString(epoch) {
		return Lease{}, ErrInvalid
	}
	query := url.Values{"memberId": {c.config.MemberID}, "processEpoch": {epoch}}
	if err := c.request(ctx, http.MethodGet, "/control?"+query.Encode(), nil, &control); err != nil {
		return Lease{}, err
	}
	if err := c.validate(control, epoch); err != nil {
		return Lease{}, err
	}
	if control.ControlRevision < 1 {
		return Lease{}, ErrInvalid
	}
	c.mu.Lock()
	c.current = control
	c.mu.Unlock()
	state := Control{Epoch: epoch, Binding: controlBinding(control), Revision: uint64(control.ControlRevision), ConfigValidUntil: control.ConfigurationLeaseUntil, LeaseValidUntil: control.ControlLeaseUntil}
	if control.DrainUntil != nil {
		state.Draining = true
		state.DrainUntil = *control.DrainUntil
	}
	return Lease{Configuration: control.Configuration, Control: state, ConfigurationSHA256: strings.TrimPrefix(control.Configuration.TreeHash, "sha256:"), CertificateValidUntil: c.certificateUntil, HardUntil: control.ServeExistingUntil, RunProcess: control.RunProcess && !control.StopRequired, AcceptNew: control.AcceptNewAuthentications, NASID: control.NASID, RuntimeProof: runtimeProof(control)}, nil
}
func (c *ControlClient) Record(ctx context.Context, observation Observation) error {
	c.mu.Lock()
	control := c.current
	c.mu.Unlock()
	if control.ControlRevision < 1 {
		return ErrDenied
	}
	if control.ProcessEpoch != observation.Epoch || !observation.ProcessStopped && uint64(control.ControlRevision) != observation.State.Revision {
		return ErrDenied
	}
	ack := api.NetworkRADIUSRuntimeACK{PoolID: control.PoolID, MemberID: control.MemberID, RuntimeID: control.RuntimeID, CredentialID: control.CredentialID, OwnerEpoch: control.OwnerEpoch, ControlRevision: control.ControlRevision, ConfigurationRevision: control.ConfigurationRevision, ConfigurationHash: control.ConfigurationHash, ProcessEpoch: control.ProcessEpoch, RuntimeVersion: "3.2.10", ObservedAt: time.Now().UTC(), StateCountKnown: observation.State.Known, ActiveStates: observation.State.Active, HooksReady: observation.HooksReady, ConfigurationApplied: observation.HooksReady, ProcessStopped: observation.ProcessStopped}
	ack.Ready = ack.HooksReady && !ack.ProcessStopped && ack.StateCountKnown
	ack.AcceptingNewAuthentications = ack.Ready && control.AcceptNewAuthentications
	// A tracked NAS request is observed separately; Pod/child readiness is not NAS-use proof.
	ack.ObservedNASUse = observation.ObservedNASUse
	var received api.NetworkRADIUSRuntimeACK
	if err := c.request(ctx, http.MethodPost, "/control/receipt", ack, &received); err != nil {
		return err
	}
	if received.PoolID != ack.PoolID || received.MemberID != ack.MemberID || received.RuntimeID != ack.RuntimeID || received.CredentialID != ack.CredentialID || received.ProcessEpoch != ack.ProcessEpoch || received.OwnerEpoch != ack.OwnerEpoch || received.ControlRevision != ack.ControlRevision || received.ConfigurationHash != ack.ConfigurationHash || received.ConfigurationRevision != ack.ConfigurationRevision {
		return ErrDenied
	}
	return nil
}
