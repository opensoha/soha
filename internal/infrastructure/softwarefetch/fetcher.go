package softwarefetch

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	appsoftware "github.com/opensoha/soha/internal/application/software"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxURLLength    = 2048
	downloadTimeout = 15 * time.Minute
)

type Fetcher struct {
	client   *http.Client
	maxBytes int64
}

func New(maxBytes int64) *Fetcher {
	networkDialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	dialer := safeDialer{lookup: net.DefaultResolver.LookupIPAddr, dial: networkDialer.DialContext}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            dialer.dialContext,
			ForceAttemptHTTP2:      true,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  30 * time.Second,
			IdleConnTimeout:        30 * time.Second,
			MaxResponseHeaderBytes: 1 << 20,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			_, err := validateURL(request.URL.String())
			return err
		},
	}
	return &Fetcher{client: client, maxBytes: maxBytes}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (appsoftware.RemoteFile, error) {
	parsed, err := validateURL(rawURL)
	if err != nil {
		return appsoftware.RemoteFile{}, err
	}
	downloadCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		cancel()
		return appsoftware.RemoteFile{}, invalidURL()
	}
	response, err := f.client.Do(request)
	if err != nil {
		cancel()
		return appsoftware.RemoteFile{}, unavailableURL()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		cancel()
		return appsoftware.RemoteFile{}, unavailableURL()
	}
	if response.ContentLength > f.maxBytes {
		response.Body.Close()
		cancel()
		return appsoftware.RemoteFile{}, fmt.Errorf("%w: installer exceeds the upload limit", apperrors.ErrInvalidArgument)
	}
	return appsoftware.RemoteFile{
		FileName: responseFileName(response),
		Content:  &cancelReadCloser{ReadCloser: response.Body, cancel: cancel},
	}, nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

type safeDialer struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (d safeDialer) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, invalidURL()
	}
	addresses, err := d.lookup(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, unavailableURL()
	}
	for _, address := range addresses {
		if isUnsafeIP(address.IP) {
			return nil, invalidURL()
		}
	}
	return d.dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func validateURL(rawURL string) (*url.URL, error) {
	if len(rawURL) == 0 || len(rawURL) > maxURLLength {
		return nil, invalidURL()
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" {
		return nil, invalidURL()
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return nil, invalidURL()
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, invalidURL()
		}
	}
	return parsed, nil
}

func isUnsafeIP(ip net.IP) bool {
	return ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate()
}

func responseFileName(response *http.Response) string {
	if _, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition")); err == nil {
		if name := baseName(params["filename"]); name != "" {
			return name
		}
	}
	return baseName(response.Request.URL.Path)
}

func baseName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	name := path.Base(strings.ReplaceAll(value, `\`, "/"))
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func invalidURL() error {
	return fmt.Errorf("%w: invalid installer URL", apperrors.ErrInvalidArgument)
}

func unavailableURL() error {
	return fmt.Errorf("%w: installer URL is unavailable", apperrors.ErrInvalidArgument)
}
