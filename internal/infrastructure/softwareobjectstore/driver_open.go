package softwareobjectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

func (d *s3Driver) OpenObject(ctx context.Context, req storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	if err := req.Validate(); err != nil {
		return storagedriver.OpenObjectResult{}, driverValidationError(err)
	}
	b, err := driverBinding(req.Binding)
	if err != nil {
		return storagedriver.OpenObjectResult{}, err
	}
	result, err := b.open(ctx, req)
	if result.Body == nil {
		b.http.CloseIdleConnections()
	}
	return result, err
}

func (b s3DriverBinding) open(ctx context.Context, req storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	input := s3.GetObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(b.config.Prefix + req.Key)}
	if req.Range == "" {
		return b.get(ctx, req, &input, nil)
	}

	// S3-compatible stores vary in how they evaluate weak tags and suffix
	// ranges. Resolve these against HEAD, then pin GET to that representation.
	head, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: input.Bucket, Key: input.Key})
	if err != nil {
		return storagedriver.OpenObjectResult{}, normalizeS3Error(err)
	}
	etag := aws.ToString(head.ETag)
	if !storagedriver.MatchesIfNoneMatch("*", etag) {
		return storagedriver.OpenObjectResult{}, driverError(storagedriver.CodeInternal)
	}
	if storagedriver.MatchesIfNoneMatch(req.IfNoneMatch, etag) {
		return storagedriver.OpenObjectResult{ETag: etag, NotModified: true}, nil
	}
	if head.ContentLength == nil || *head.ContentLength < 0 {
		return storagedriver.OpenObjectResult{}, driverError(storagedriver.CodeInternal)
	}
	interval, err := storagedriver.ResolveByteRange(req.Range, *head.ContentLength)
	if err != nil {
		return storagedriver.OpenObjectResult{}, err
	}
	input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", interval.Start, interval.End))
	input.IfMatch = head.ETag
	return b.get(ctx, req, &input, interval)
}

func (b s3DriverBinding) get(
	ctx context.Context,
	req storagedriver.OpenObjectRequest,
	input *s3.GetObjectInput,
	interval *storagedriver.ContentRange,
) (storagedriver.OpenObjectResult, error) {
	output, err := b.client.GetObject(ctx, input)
	if err != nil {
		return storagedriver.OpenObjectResult{}, normalizeS3Error(err)
	}
	result, err := b.openResult(req, output, input, interval)
	if result.Body == nil {
		discardOpenBody(output.Body)
	}
	return result, err
}

func (b s3DriverBinding) openResult(
	req storagedriver.OpenObjectRequest,
	output *s3.GetObjectOutput,
	input *s3.GetObjectInput,
	interval *storagedriver.ContentRange,
) (storagedriver.OpenObjectResult, error) {
	etag := aws.ToString(output.ETag)
	if input.IfMatch != nil && etag != *input.IfMatch {
		return storagedriver.OpenObjectResult{}, driverError(storagedriver.CodeUnavailable)
	}
	if storagedriver.MatchesIfNoneMatch(req.IfNoneMatch, etag) {
		return storagedriver.OpenObjectResult{ETag: etag, NotModified: true}, nil
	}
	var expectedRange string
	if interval != nil {
		expectedRange = fmt.Sprintf("bytes %d-%d/%d", interval.Start, interval.End, interval.Size)
	}
	if output.ContentLength == nil || output.Body == nil || aws.ToString(output.ContentRange) != expectedRange {
		return storagedriver.OpenObjectResult{}, driverError(storagedriver.CodeInternal)
	}
	result := storagedriver.OpenObjectResult{
		ETag: etag, ContentLength: *output.ContentLength, ContentType: aws.ToString(output.ContentType),
		ContentRange: interval, Body: &driverOpenBody{body: output.Body, remaining: *output.ContentLength, http: b.http},
	}
	if err := result.Validate(req); err != nil {
		return storagedriver.OpenObjectResult{}, driverError(storagedriver.CodeInternal)
	}
	return result, nil
}

func discardOpenBody(body io.ReadCloser) {
	if body != nil {
		// No object bytes are forwarded on this path.
		_ = body.Close()
	}
}

type driverOpenBody struct {
	body      io.ReadCloser
	remaining int64
	http      *http.Client
}

func (r *driverOpenBody) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.body.Read(p)
	r.remaining -= int64(n)
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, io.EOF) && r.remaining == 0:
		return n, io.EOF
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return n, io.ErrUnexpectedEOF
	default:
		return n, normalizeS3Error(err)
	}
}

func (r *driverOpenBody) Close() error {
	defer r.http.CloseIdleConnections()
	if err := r.body.Close(); err != nil {
		return normalizeS3Error(err)
	}
	return nil
}
