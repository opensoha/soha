package softwareobjectstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

func (d *s3Driver) ListPrefix(ctx context.Context, req storagedriver.ListPrefixRequest) (storagedriver.ListPrefixResult, error) {
	if err := req.Validate(); err != nil {
		return storagedriver.ListPrefixResult{}, driverValidationError(err)
	}
	config, err := parseDriverConfig(req.Binding)
	if err != nil {
		return storagedriver.ListPrefixResult{}, err
	}
	scope := listCursorScope(config, req.Prefix)
	token, err := d.decodeListCursor(scope, req.Cursor)
	if err != nil {
		return storagedriver.ListPrefixResult{}, err
	}

	b := newS3DriverBinding(config)
	defer b.http.CloseIdleConnections()
	fullPrefix := b.config.Prefix + req.Prefix
	limit := int32(req.Limit) // #nosec G115 -- Request validation above constrains the limit to the protocol's MaxPageSize.
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(b.config.Bucket), Prefix: aws.String(fullPrefix),
		MaxKeys: aws.Int32(limit), EncodingType: types.EncodingTypeUrl,
	}
	if token != "" {
		input.ContinuationToken = aws.String(token)
	}
	output, err := b.client.ListObjectsV2(ctx, input)
	if err != nil {
		return storagedriver.ListPrefixResult{}, normalizeS3ListError(err, token != "")
	}
	return d.listResult(req, fullPrefix, scope, output)
}

func (d *s3Driver) listResult(
	req storagedriver.ListPrefixRequest,
	fullPrefix string,
	scope []byte,
	output *s3.ListObjectsV2Output,
) (storagedriver.ListPrefixResult, error) {
	if len(output.Contents) > req.Limit {
		return storagedriver.ListPrefixResult{}, driverError(storagedriver.CodeInternal)
	}
	result := storagedriver.ListPrefixResult{Keys: make([]string, 0, len(output.Contents))}
	for _, object := range output.Contents {
		key := aws.ToString(object.Key)
		if output.EncodingType == types.EncodingTypeUrl {
			decoded, err := url.PathUnescape(key)
			if err != nil {
				return storagedriver.ListPrefixResult{}, driverError(storagedriver.CodeInternal)
			}
			key = decoded
		}
		if !strings.HasPrefix(key, fullPrefix) {
			return storagedriver.ListPrefixResult{}, driverError(storagedriver.CodeInternal)
		}
		result.Keys = append(result.Keys, strings.TrimPrefix(key, req.Binding.Prefix))
	}
	if aws.ToBool(output.IsTruncated) {
		token := aws.ToString(output.NextContinuationToken)
		if token == "" {
			return storagedriver.ListPrefixResult{}, driverError(storagedriver.CodeInternal)
		}
		result.NextCursor = d.encodeListCursor(scope, token)
	}
	if err := result.Validate(req); err != nil {
		return storagedriver.ListPrefixResult{}, driverError(storagedriver.CodeInternal)
	}
	return result, nil
}

func normalizeS3ListError(err error, usedCursor bool) error {
	code := normalizeS3Error(err)
	if !usedCursor || code.Code == storagedriver.CodeAccessDenied {
		return code
	}
	if providerStatus(err) == http.StatusBadRequest {
		return driverError(storagedriver.CodeInvalidCursor)
	}
	return code
}

func listCursorScope(config Config, prefix string) []byte {
	// Scope includes the exact connection and its provider destination. A
	// credential rotation does not change the connection's list namespace.
	raw, _ := json.Marshal([]any{
		config.IntegrationID, config.Bucket, config.Prefix, config.Endpoint,
		config.Region, config.PathStyle, config.Insecure, config.AllowPrivate, prefix,
	})
	return raw
}

func (d *s3Driver) encodeListCursor(scope []byte, token string) string {
	return base64.RawURLEncoding.EncodeToString(append(d.listCursorMAC(scope, token), token...))
}

func (d *s3Driver) decodeListCursor(scope []byte, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || len(data) <= sha256.Size {
		return "", driverError(storagedriver.CodeInvalidCursor)
	}
	token := string(data[sha256.Size:])
	if !hmac.Equal(data[:sha256.Size], d.listCursorMAC(scope, token)) {
		return "", driverError(storagedriver.CodeInvalidCursor)
	}
	return token, nil
}

func (d *s3Driver) listCursorMAC(scope []byte, token string) []byte {
	mac := hmac.New(sha256.New, d.cursorKey[:])
	_, _ = mac.Write(scope)
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(token))
	return mac.Sum(nil)
}
