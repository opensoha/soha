package softwareobjectstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

type s3Driver struct {
	cursorKey [32]byte
}

// NewS3Driver creates the built-in S3 driver. Each call uses only the
// credentials carried by that call's binding.
func NewS3Driver() storagedriver.Driver {
	d := &s3Driver{}
	rand.Read(d.cursorKey[:])
	return d
}

type s3DriverBinding struct {
	config Config
	client *s3.Client
	http   *http.Client
}

var (
	driverBucketPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	driverRegionPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)
)

func parseDriverConfig(b storagedriver.Binding) (Config, error) {
	fields, err := driverSettings(b.Settings)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		IntegrationID: b.ID,
		Bucket:        b.Container,
		Prefix:        b.Prefix,
		Endpoint:      strings.TrimRight(fields["endpoint"], "/"),
		Region:        fields["region"],
		AccessKeyID:   b.Credentials["access_key_id"],
		SecretKey:     b.Credentials["secret_access_key"],
		SessionToken:  b.Credentials["session_token"],
	}
	if !driverBucketPattern.MatchString(config.Bucket) || config.Bucket == "." || config.Bucket == ".." ||
		!driverRegionPattern.MatchString(config.Region) || !validDriverEndpoint(config.Endpoint) {
		return Config{}, driverError(storagedriver.CodeInvalidRequest)
	}
	if err := parseConnectionFlags(&config, fields); err != nil {
		return Config{}, driverError(storagedriver.CodeInvalidRequest)
	}
	if err := validateConnectionConfig(config); err != nil {
		return Config{}, driverError(storagedriver.CodeInvalidRequest)
	}
	return config, nil
}

func driverSettings(settings json.RawMessage) (map[string]string, error) {
	fields := make(map[string]string)
	if len(settings) == 0 {
		return fields, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(settings, &raw); err != nil {
		return nil, driverError(storagedriver.CodeInvalidRequest)
	}
	for key, value := range raw {
		switch key {
		case "endpoint", "region", "path_style", "bucket_endpoint", "insecure", "allow_private":
		default:
			return nil, driverError(storagedriver.CodeInvalidRequest)
		}
		var field *string
		if err := json.Unmarshal(value, &field); err != nil || field == nil {
			return nil, driverError(storagedriver.CodeInvalidRequest)
		}
		fields[key] = strings.TrimSpace(*field)
	}
	return fields, nil
}

func validDriverEndpoint(endpoint string) bool {
	if endpoint == "" {
		return true
	}
	u, err := url.Parse(endpoint)
	return err == nil && u.User == nil && !u.ForceQuery && u.RawQuery == "" && u.Fragment == ""
}

func newS3DriverBinding(config Config) s3DriverBinding {
	transport := storageHTTPTransport(config.AllowPrivate)
	// A stored object's bytes and range offsets refer to its encoded form.
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 30 * time.Second
	httpClient := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	client := newS3Client(config, httpClient, func(o *s3.Options) {
		o.DisableS3ExpressSessionAuth = aws.Bool(true)
		o.DisableMultiRegionAccessPoints = true
		// A caller decides whether to replay a write with an uncertain result.
		o.Retryer = aws.NopRetryer{}
	})
	return s3DriverBinding{config: config, client: client, http: httpClient}
}

func driverBinding(binding storagedriver.Binding) (s3DriverBinding, error) {
	config, err := parseDriverConfig(binding)
	if err != nil {
		return s3DriverBinding{}, err
	}
	return newS3DriverBinding(config), nil
}

func (d *s3Driver) TestConnection(ctx context.Context, binding storagedriver.Binding) error {
	if err := binding.Validate(); err != nil {
		return driverValidationError(err)
	}
	b, err := driverBinding(binding)
	if err != nil {
		return err
	}
	defer b.http.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	random := make([]byte, 32)
	rand.Read(random)
	probe := storagedriver.PutObjectRequest{
		Binding: binding, Key: base64.RawURLEncoding.EncodeToString(random[:16]),
		ContentType: storagedriver.MediaTypeBinary,
	}
	if err := probe.Validate(); err != nil {
		return driverValidationError(err)
	}
	return b.probe(ctx, probe, random[16:])
}

func (b s3DriverBinding) probe(ctx context.Context, req storagedriver.PutObjectRequest, payload []byte) error {
	bucket := aws.String(b.config.Bucket)
	key := aws.String(b.config.Prefix + req.Key)
	output, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: bucket, Key: key, Body: bytes.NewReader(payload),
		ContentType: aws.String(req.ContentType), IfNoneMatch: aws.String("*"),
	})
	if err != nil {
		b.recoverProbe(ctx, bucket, key, payload, err)
		return normalizeS3Error(err)
	}
	if !storagedriver.MatchesIfNoneMatch("*", aws.ToString(output.ETag)) {
		output.ETag = nil
	}

	read, readErr := b.readProbe(ctx, &s3.GetObjectInput{
		Bucket: bucket, Key: key, IfMatch: output.ETag,
	}, payload)
	if output.ETag == nil && readErr == nil {
		output.ETag = read.ETag
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	deleteErr := b.deleteProbe(cleanup, &s3.DeleteObjectInput{
		Bucket: bucket, Key: key, VersionId: output.VersionId, IfMatch: output.ETag,
	})
	if readErr != nil {
		return readErr
	}
	return deleteErr
}

func (b s3DriverBinding) readProbe(ctx context.Context, request *s3.GetObjectInput, payload []byte) (*s3.GetObjectOutput, error) {
	read, err := b.client.GetObject(ctx, request)
	if err != nil {
		return nil, normalizeS3Error(err)
	}
	found, readErr := io.ReadAll(io.LimitReader(read.Body, int64(len(payload)+1)))
	closeErr := read.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, normalizeS3Error(err)
	}
	if !bytes.Equal(payload, found) {
		return nil, driverError(storagedriver.CodeInternal)
	}
	return read, nil
}

func (b s3DriverBinding) deleteProbe(ctx context.Context, request *s3.DeleteObjectInput) error {
	if !storagedriver.MatchesIfNoneMatch("*", aws.ToString(request.IfMatch)) {
		request.IfMatch = nil
	}
	if aws.ToString(request.VersionId) == "" && request.IfMatch == nil {
		// Cleanup must target an observed representation of the temporary object.
		return driverError(storagedriver.CodeInternal)
	}
	if _, err := b.client.DeleteObject(ctx, request); err != nil && !absentS3Key(err) {
		return normalizeS3Error(err)
	}
	return nil
}

func (b s3DriverBinding) recoverProbe(ctx context.Context, bucket, key *string, payload []byte, putErr error) {
	if status := providerStatus(putErr); status >= 400 && status < 500 {
		return
	}
	// An ambiguous PUT may have succeeded. Recover only our own payload, and
	// delete only the version or ETag observed during the recovery read.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	read, err := b.readProbe(cleanup, &s3.GetObjectInput{Bucket: bucket, Key: key}, payload)
	if err == nil {
		_ = b.deleteProbe(cleanup, &s3.DeleteObjectInput{
			Bucket: bucket, Key: key, VersionId: read.VersionId, IfMatch: read.ETag,
		})
	}
}

func (d *s3Driver) DeleteKeys(ctx context.Context, req storagedriver.DeleteKeysRequest) error {
	if err := req.Validate(); err != nil {
		return driverValidationError(err)
	}
	b, err := driverBinding(req.Binding)
	if err != nil {
		return err
	}
	defer b.http.CloseIdleConnections()

	seen := make(map[string]struct{}, len(req.Keys))
	for _, key := range req.Keys {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(b.config.Bucket), Key: aws.String(b.config.Prefix + key),
		})
		if err == nil || absentS3Key(err) {
			continue
		}
		return normalizeS3Error(err)
	}
	return nil
}

func driverError(code storagedriver.ErrorCode) storagedriver.RPCError {
	return storagedriver.RPCError{Code: code}
}

func driverValidationError(err error) storagedriver.RPCError {
	var rpc storagedriver.RPCError
	if errors.As(err, &rpc) && rpc.Code.HTTPStatus() != 0 {
		return rpc
	}
	return driverError(storagedriver.CodeInvalidRequest)
}

func providerErrorCode(err error) string {
	var api interface{ ErrorCode() string }
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}

func providerStatus(err error) int {
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}

func absentS3Key(err error) bool {
	code := providerErrorCode(err)
	return code == "NoSuchKey" || code == "NotFound" || code == "" && providerStatus(err) == http.StatusNotFound
}

func normalizeS3Error(err error) storagedriver.RPCError {
	var rpc storagedriver.RPCError
	if errors.As(err, &rpc) && rpc.Code.HTTPStatus() != 0 {
		return rpc
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return driverError(storagedriver.CodeDeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return driverError(storagedriver.CodeUnavailable)
	}
	var transport net.Error
	if errors.As(err, &transport) && transport.Timeout() {
		return driverError(storagedriver.CodeDeadlineExceeded)
	}
	switch providerErrorCode(err) {
	case "AccessDenied", "InvalidAccessKeyId", "InvalidToken", "ExpiredToken", "SignatureDoesNotMatch":
		return driverError(storagedriver.CodeAccessDenied)
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return driverError(storagedriver.CodeNotFound)
	case "EntityTooLarge":
		return driverError(storagedriver.CodeTooLarge)
	case "SlowDown", "Throttling", "ThrottlingException":
		return driverError(storagedriver.CodeBusy)
	}
	switch providerStatus(err) {
	case http.StatusUnauthorized, http.StatusForbidden:
		return driverError(storagedriver.CodeAccessDenied)
	case http.StatusNotFound:
		return driverError(storagedriver.CodeNotFound)
	case http.StatusRequestEntityTooLarge:
		return driverError(storagedriver.CodeTooLarge)
	case http.StatusRequestedRangeNotSatisfiable:
		return driverError(storagedriver.CodeRangeNotSatisfiable)
	case http.StatusTooManyRequests:
		return driverError(storagedriver.CodeBusy)
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return driverError(storagedriver.CodeDeadlineExceeded)
	case http.StatusBadRequest:
		return driverError(storagedriver.CodeInvalidRequest)
	}
	return driverError(storagedriver.CodeUnavailable)
}
