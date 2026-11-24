package response

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	contractsresponse "github.com/opensoha/soha/contracts/apiresponse"
)

func AcceptsGzip(headers []string) bool {
	acceptsWildcard := false
	for _, header := range headers {
		for _, entry := range strings.Split(header, ",") {
			name, params, _ := strings.Cut(entry, ";")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "gzip":
				return acceptsEncodingWeight(params)
			case "*":
				acceptsWildcard = acceptsEncodingWeight(params)
			}
		}
	}
	return acceptsWildcard
}

func acceptsEncodingWeight(params string) bool {
	name, value, exists := strings.Cut(strings.TrimSpace(params), "=")
	if !exists {
		return name == ""
	}
	q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return strings.EqualFold(strings.TrimSpace(name), "q") && err == nil && q > 0 && q <= 1
}

func GzipItem(c *gin.Context, data any) {
	GzipJSON(c, contractsresponse.Item(data))
}

// GzipJSON is for successful, bounded reads with no credentials or reflected request data.
func GzipJSON(c *gin.Context, payload any) {
	c.Writer.Header().Add("Vary", "Accept-Encoding")
	if !AcceptsGzip(c.Request.Header.Values("Accept-Encoding")) {
		JSON(c, http.StatusOK, payload)
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		JSON(c, http.StatusOK, payload)
		return
	}
	if len(body) >= 1024 {
		if compressed, ok := gzipBody(body); ok {
			c.Header("Content-Encoding", "gzip")
			body = compressed
		}
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func gzipBody(body []byte) ([]byte, bool) {
	var encoded bytes.Buffer
	zip, _ := gzip.NewWriterLevel(&encoded, gzip.BestSpeed)
	_, writeErr := zip.Write(body)
	closeErr := zip.Close()
	return encoded.Bytes(), writeErr == nil && closeErr == nil && encoded.Len() < len(body)
}
