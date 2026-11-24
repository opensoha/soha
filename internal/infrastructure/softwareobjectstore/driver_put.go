package softwareobjectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

const (
	driverPartSize = 8 << 20
	maxDriverParts = 10000
)

func (d *s3Driver) PutObject(ctx context.Context, req storagedriver.PutObjectRequest, body io.Reader) (storagedriver.PutObjectResult, error) {
	if err := req.Validate(); err != nil {
		return storagedriver.PutObjectResult{}, driverValidationError(err)
	}
	if body == nil {
		return storagedriver.PutObjectResult{}, driverError(storagedriver.CodeInvalidRequest)
	}
	b, err := driverBinding(req.Binding)
	if err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	defer b.http.CloseIdleConnections()

	upload := s3DriverUpload{
		client: b.client,
		object: s3.PutObjectInput{
			Bucket: aws.String(b.config.Bucket), Key: aws.String(b.config.Prefix + req.Key),
			ContentType: aws.String(req.ContentType),
		},
	}
	return upload.put(ctx, body)
}

type s3DriverUpload struct {
	client *s3.Client
	object s3.PutObjectInput
}

func (u s3DriverUpload) put(ctx context.Context, source io.Reader) (storagedriver.PutObjectResult, error) {
	hash := sha256.New()
	body := &driverUploadBody{ctx: ctx, body: io.TeeReader(source, hash)}
	part, err := io.ReadAll(io.LimitReader(body, driverPartSize))
	if err != nil {
		return storagedriver.PutObjectResult{}, driverReadError(err)
	}

	var etag string
	if body.eof {
		etag, err = u.putSingle(ctx, part)
	} else {
		etag, err = u.putMultipart(ctx, body, part)
	}
	if err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	result := storagedriver.PutObjectResult{
		Size: body.size, SHA256: hex.EncodeToString(hash.Sum(nil)), ETag: etag,
	}
	if !body.eof || result.Validate() != nil {
		return storagedriver.PutObjectResult{}, driverError(storagedriver.CodeInternal)
	}
	return result, nil
}

func readUploadPart(body *driverUploadBody, buffer []byte) ([]byte, error) {
	n, err := io.ReadFull(body, buffer)
	// ReadFull can suppress a read error when the same read fills the part.
	if body.readErr != nil {
		return nil, driverReadError(body.readErr)
	}
	if err != nil && !body.eof {
		return nil, driverReadError(err)
	}
	return buffer[:n], nil
}

func (u s3DriverUpload) putSingle(ctx context.Context, part []byte) (string, error) {
	input := u.object
	input.Body = bytes.NewReader(part)
	input.ContentLength = aws.Int64(int64(len(part)))
	output, err := u.client.PutObject(ctx, &input)
	if err != nil {
		return "", normalizeS3Error(err)
	}
	return aws.ToString(output.ETag), nil
}

func (u s3DriverUpload) putMultipart(ctx context.Context, body *driverUploadBody, first []byte) (etag string, err error) {
	// Manage bounded S3 parts directly so failures never log raw provider
	// errors or upload IDs from the SDK's higher-level transfer manager.
	create, err := u.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: u.object.Bucket, Key: u.object.Key, ContentType: u.object.ContentType,
	})
	if err != nil {
		return "", normalizeS3Error(err)
	}
	if aws.ToString(create.UploadId) == "" {
		return "", driverError(storagedriver.CodeInternal)
	}
	defer func() {
		if err != nil {
			u.abortMultipart(ctx, create.UploadId)
		}
	}()

	parts := make([]types.CompletedPart, 0)
	var number int32 = 1
	for part := first; len(part) > 0; {
		if len(parts) >= maxDriverParts {
			return "", driverError(storagedriver.CodeTooLarge)
		}
		completed, err := u.uploadPart(ctx, create.UploadId, number, part)
		if err != nil {
			return "", err
		}
		parts = append(parts, completed)
		number++
		if body.eof {
			break
		}
		part, err = readUploadPart(body, first)
		if err != nil {
			return "", err
		}
	}
	output, err := u.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: u.object.Bucket, Key: u.object.Key, UploadId: create.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return "", normalizeS3Error(err)
	}
	return aws.ToString(output.ETag), nil
}

func (u s3DriverUpload) uploadPart(ctx context.Context, uploadID *string, number int32, part []byte) (types.CompletedPart, error) {
	output, err := u.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: u.object.Bucket, Key: u.object.Key, UploadId: uploadID,
		PartNumber: &number, Body: bytes.NewReader(part), ContentLength: aws.Int64(int64(len(part))),
	})
	if err != nil {
		return types.CompletedPart{}, normalizeS3Error(err)
	}
	if !storagedriver.MatchesIfNoneMatch("*", aws.ToString(output.ETag)) {
		return types.CompletedPart{}, driverError(storagedriver.CodeInternal)
	}
	return types.CompletedPart{PartNumber: &number, ETag: output.ETag}, nil
}

func (u s3DriverUpload) abortMultipart(ctx context.Context, uploadID *string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	// Preserve the original failure; abort targets only this upload ID.
	_, _ = u.client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{
		Bucket: u.object.Bucket, Key: u.object.Key, UploadId: uploadID,
	})
}

type driverUploadBody struct {
	ctx     context.Context
	body    io.Reader
	size    int64
	eof     bool
	readErr error
}

func (r *driverUploadBody) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.body.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		r.readErr = canceled
		return 0, canceled
	}
	if int64(n) > math.MaxInt64-r.size {
		err = driverError(storagedriver.CodeTooLarge)
		r.readErr = err
		return 0, err
	}
	r.size += int64(n)
	if err == io.EOF {
		r.eof = true
	} else if err != nil {
		r.readErr = err
	}
	return n, err
}

func driverReadError(err error) error {
	code := normalizeS3Error(err)
	if code.Code == storagedriver.CodeUnavailable && !errors.Is(err, context.Canceled) {
		return driverError(storagedriver.CodeInternal)
	}
	return code
}
