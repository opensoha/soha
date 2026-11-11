package guest

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

// Only the private browser controller sends these frames over current mTLS.
type BrowserNetworkRequest struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodyBase64 string            `json:"bodyBase64,omitempty"`
}
type BrowserNetworkHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type BrowserNetworkResponse struct {
	Status     int                    `json:"status"`
	Headers    []BrowserNetworkHeader `json:"headers"`
	BodyBase64 string                 `json:"bodyBase64"`
}

func (v BrowserNetworkResponse) Validate() error {
	if v.Status < 100 || v.Status > 599 || len(v.Headers) > 128 {
		return errors.New("invalid browser network receipt")
	}
	data, err := base64.StdEncoding.DecodeString(v.BodyBase64)
	if err != nil || len(data) > 1<<20 {
		return errors.New("browser response exceeds its bound")
	}
	for _, h := range v.Headers {
		if h.Name == "" || len(h.Name) > 128 || len(h.Value) > 8192 || strings.ContainsAny(h.Name+h.Value, "\x00\r\n") {
			return errors.New("invalid browser response header")
		}
	}
	return nil
}

func (v BrowserNetworkRequest) Validate() error {
	u, err := url.Parse(v.URL)
	if err != nil || !BrowserURL(v.URL) || u.Scheme != "https" || u.Fragment != "" || u.Opaque != "" || len(v.Headers) > 64 {
		return errors.New("invalid browser network request")
	}
	switch v.Method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		return errors.New("unsupported browser network method")
	}
	body, err := base64.StdEncoding.DecodeString(v.BodyBase64)
	if err != nil || len(body) > 64<<10 || (v.Method == "GET" || v.Method == "HEAD") && len(body) > 0 {
		return errors.New("browser network body exceeds its bound")
	}
	size := 0
	for name, value := range v.Headers {
		size += len(name) + len(value)
		if name == "" || strings.ContainsAny(name+value, "\x00\r\n") || len(name) > 128 || len(value) > 8192 {
			return errors.New("invalid browser network header")
		}
	}
	if size > 16<<10 {
		return errors.New("browser network headers exceed their bound")
	}
	return nil
}
func (v *BrowserNetworkRequest) UnmarshalJSON(raw []byte) error {
	type wire BrowserNetworkRequest
	var decoded wire
	if err := decodeStrictBounded(raw, &decoded, 128<<10, "url", "method", "headers", "bodyBase64"); err != nil {
		return err
	}
	*v = BrowserNetworkRequest(decoded)
	return v.Validate()
}
