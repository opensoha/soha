package appgateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

// ObjectStream wraps a streamed static asset body and metadata from object storage.
type ObjectStream struct {
	Body          io.ReadCloser
	ContentLength int64
	ContentType   string
	ETag          string
}

// ObjectReader reads static bundle objects from private MinIO/S3 storage.
type ObjectReader interface {
	GetObject(ctx context.Context, key string) (*ObjectStream, error)
}

type s3GetObjectAPI interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// S3ObjectReader implements ObjectReader against S3/MinIO with dynamic file-backed credential reload.
type S3ObjectReader struct {
	bucket string
	client s3GetObjectAPI
}

// NewS3ObjectReader constructs a read-only S3/MinIO ObjectReader from gateway Config.
func NewS3ObjectReader(cfg Config) (*S3ObjectReader, error) {
	secrets := newSecretProvider(cfg)
	return newS3ObjectReaderWithSecrets(cfg, secrets)
}

func newS3ObjectReaderWithSecrets(cfg Config, secrets *secretProvider) (*S3ObjectReader, error) {
	if strings.TrimSpace(cfg.S3Bucket) == "" || strings.TrimSpace(cfg.S3Endpoint) == "" {
		return nil, errors.New("S3 endpoint and bucket are required")
	}
	if secrets == nil {
		return nil, errors.New("secret provider is required")
	}
	creds := aws.CredentialsProviderFunc(func(_ context.Context) (aws.Credentials, error) {
		accessKey, secretKey, err := secrets.S3Credentials()
		if err != nil {
			return aws.Credentials{}, err
		}
		return aws.Credentials{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			Source:          "soha-app-gateway",
		}, nil
	})
	awsCfg := aws.Config{
		Region:                     defaultString(strings.TrimSpace(cfg.S3Region), DefaultS3Region),
		Credentials:                creds,
		HTTPClient:                 newS3HTTPClient(cfg.S3Insecure),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.S3Endpoint), "/")
	pathStyle := cfg.S3PathStyle
	client := s3.NewFromConfig(awsCfg, func(opts *s3.Options) {
		opts.UsePathStyle = pathStyle
		opts.BaseEndpoint = aws.String(endpoint)
	})
	return &S3ObjectReader{
		bucket: strings.TrimSpace(cfg.S3Bucket),
		client: client,
	}, nil
}

// GetObject fetches an object stream by exact bucket key from S3/MinIO.
func (r *S3ObjectReader) GetObject(ctx context.Context, key string) (*ObjectStream, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("S3 object reader is not initialized")
	}
	cleaned, ok := cleanStorageKey(key)
	if !ok {
		return nil, errors.New("invalid object storage key")
	}
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(cleaned),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get object: %w", err)
	}
	size := int64(-1)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return &ObjectStream{
		Body:          out.Body,
		ContentLength: size,
		ContentType:   aws.ToString(out.ContentType),
		ETag:          aws.ToString(out.ETag),
	}, nil
}

func newS3HTTPClient(insecureTLS bool) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
	}
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // G402: explicit operator opt-in via SOHA_APP_GATEWAY_S3_INSECURE for internal MinIO testing
		}
	}
	return &http.Client{Transport: transport}
}

func resolveObjectKey(gatewayPrefix, decisionPrefix, fileObjectKey string) (string, error) {
	objKey, ok := cleanStorageKey(fileObjectKey)
	if !ok {
		return "", errors.New("invalid manifest object key")
	}
	decPrefix, ok := cleanStoragePrefix(decisionPrefix)
	if !ok {
		return "", errors.New("invalid decision storage prefix")
	}
	gwPrefix, ok := cleanStoragePrefix(gatewayPrefix)
	if !ok {
		return "", errors.New("invalid gateway S3 prefix")
	}
	combined := prependPrefixOnce(decPrefix, objKey)
	combined = prependPrefixOnce(gwPrefix, combined)
	return combined, nil
}

func prependPrefixOnce(prefix, key string) string {
	if prefix == "" || key == prefix || strings.HasPrefix(key, prefix+"/") {
		return key
	}
	return prefix + "/" + key
}

func cleanStoragePrefix(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "", true
	}
	return cleanStorageKey(trimmed)
}

func cleanStorageKey(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" || strings.HasSuffix(trimmed, "/") || strings.Contains(trimmed, "\\") || containsControlChar(trimmed) {
		return "", false
	}
	segments := strings.Split(trimmed, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return trimmed, true
}

func resolveManifestFile(urlPath string, files []sohaapi.HostedAppManifestFile, spaFallback bool) (sohaapi.HostedAppManifestFile, bool) {
	if len(files) == 0 || strings.Contains(urlPath, "\\") || containsControlChar(urlPath) {
		return sohaapi.HostedAppManifestFile{}, false
	}
	index := make(map[string]sohaapi.HostedAppManifestFile, len(files))
	for _, f := range files {
		cleanRel, ok := cleanManifestPath(f.Path)
		if ok {
			index[cleanRel] = f
		}
	}
	if urlPath == "" || urlPath == "/" {
		f, ok := index["index.html"]
		return f, ok
	}
	hadTrailingSlash := strings.HasSuffix(urlPath, "/")
	cleaned := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if cleaned == "" {
		f, ok := index["index.html"]
		return f, ok
	}
	if !hadTrailingSlash {
		if exact, ok := index[cleaned]; ok {
			return exact, true
		}
	}
	if dirIndex, ok := index[cleaned+"/index.html"]; ok && (hadTrailingSlash || path.Ext(cleaned) == "") {
		return dirIndex, true
	}
	if spaFallback && path.Ext(cleaned) == "" && !hasHiddenDotSegment(cleaned) {
		f, ok := index["index.html"]
		return f, ok
	}
	return sohaapi.HostedAppManifestFile{}, false
}

func cleanManifestPath(raw string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "/")
	if trimmed == "" || strings.Contains(trimmed, "\\") || containsControlChar(trimmed) {
		return "", false
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != trimmed {
		return "", false
	}
	return cleaned, true
}

func hasHiddenDotSegment(relPath string) bool {
	for _, seg := range strings.Split(relPath, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func computeETag(file sohaapi.HostedAppManifestFile) string {
	raw := strings.TrimSpace(file.Etag)
	if raw != "" {
		if (strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"")) ||
			(strings.HasPrefix(raw, "W/\"") && strings.HasSuffix(raw, "\"")) {
			return raw
		}
		return "\"" + strings.Trim(raw, "\"") + "\""
	}
	sha := strings.TrimSpace(file.Sha256)
	if sha != "" {
		return "\"sha256:" + sha + "\""
	}
	return ""
}

func ifNoneMatchHit(headerValues []string, etag string) bool {
	if etag == "" || len(headerValues) == 0 {
		return false
	}
	targetTag := stripWeakETagPrefix(etag)
	if targetTag == "" {
		return false
	}
	for _, headerVal := range headerValues {
		for _, candidate := range strings.Split(headerVal, ",") {
			trimmed := strings.TrimSpace(candidate)
			if trimmed == "*" {
				return true
			}
			if stripWeakETagPrefix(trimmed) == targetTag {
				return true
			}
		}
	}
	return false
}

func stripWeakETagPrefix(tag string) string {
	trimmed := strings.TrimSpace(tag)
	if strings.HasPrefix(trimmed, "W/") || strings.HasPrefix(trimmed, "w/") {
		trimmed = trimmed[2:]
	}
	if len(trimmed) >= 2 && strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
		return trimmed
	}
	return ""
}

// SanitizeUpstreamHeaders returns a cloned HTTP header map stripped of cookies, auth, and X-Soha-* headers.
func SanitizeUpstreamHeaders(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for name, values := range in {
		lower := strings.ToLower(strings.TrimSpace(name))
		if lower == "cookie" || lower == "authorization" || lower == "proxy-authorization" || strings.HasPrefix(lower, "x-soha-") {
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[name] = copied
	}
	return out
}
