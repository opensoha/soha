package softwarefetch

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestValidateURL(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		ok   bool
	}{
		{name: "https", url: "https://downloads.example.com/app.pkg", ok: true},
		{name: "http", url: "http://downloads.example.com/app.pkg"},
		{name: "credentials", url: "https://token@downloads.example.com/app.pkg"},
		{name: "localhost", url: "https://localhost/app.pkg"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateURL(test.url)
			if (err == nil) != test.ok {
				t.Fatalf("validateURL() error = %v, want success %v", err, test.ok)
			}
		})
	}
}

func TestSafeDialerRejectsPrivateAndPinsPublicAddress(t *testing.T) {
	dialed := ""
	dialer := safeDialer{
		lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
			if host == "private.example" {
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
		dial: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = address
			client, server := net.Pipe()
			server.Close()
			return client, nil
		},
	}
	if _, err := dialer.dialContext(context.Background(), "tcp", "private.example:443"); err == nil {
		t.Fatal("expected private address to be rejected")
	}
	connection, err := dialer.dialContext(context.Background(), "tcp", "public.example:443")
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if dialed != "203.0.113.10:443" {
		t.Fatalf("unexpected pinned address %q", dialed)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFetchUsesResponseFileName(t *testing.T) {
	fetcher := &Fetcher{
		maxBytes: 1024,
		client: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Disposition": {`attachment; filename="release.pkg"`}},
				Body:       io.NopCloser(strings.NewReader("payload")),
				Request:    request,
			}, nil
		})},
	}
	remote, err := fetcher.Fetch(context.Background(), "https://downloads.example.com/opaque")
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Content.Close()
	if remote.FileName != "release.pkg" {
		t.Fatalf("unexpected file name %q", remote.FileName)
	}
}
