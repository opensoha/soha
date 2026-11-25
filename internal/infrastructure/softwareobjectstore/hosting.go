package softwareobjectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const s3DeleteBatchSize = 1000

type s3BatchDeleter interface {
	DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

type s3PrefixLister interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// PutObject uploads a static hosting asset with explicit Content-Type and returns
// its exact byte length, lowercase SHA-256 hex digest, and normalized HTTP ETag.
func (p *Provider) PutObject(ctx context.Context, integrationID, key, contentType string, content io.Reader) (int64, string, string, error) {
	store, err := p.resolve(ctx, integrationID, true)
	if err != nil {
		return 0, "", "", err
	}
	return store.PutObject(ctx, key, contentType, content)
}

// PresignHostedSource restricts the build pool to reading one validated source snapshot.
func (p *Provider) PresignHostedSource(ctx context.Context, integrationID, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", apperrors.ErrInvalidArgument
	}
	store, err := p.resolve(ctx, integrationID, true)
	if err != nil {
		return "", err
	}
	client, ok := store.client.(*s3.Client)
	if !ok {
		return "", apperrors.ErrServiceUnavailable
	}
	request, err := s3.NewPresignClient(client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.config.Bucket), Key: aws.String(store.objectKey(key)),
	}, s3.WithPresignExpires(10*time.Minute))
	if err != nil {
		return "", errors.New("failed to sign hosted source snapshot")
	}
	return request.URL, nil
}

// DeleteKeys removes static hosting objects for evicted or failed revisions.
func (p *Provider) DeleteKeys(ctx context.Context, integrationID string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	store, err := p.resolve(ctx, integrationID, false)
	if err != nil {
		return err
	}
	return store.DeleteKeys(ctx, keys)
}

// DeletePrefix lists and removes all objects beneath a revision prefix.
func (p *Provider) DeletePrefix(ctx context.Context, integrationID, prefix string) error {
	store, err := p.resolve(ctx, integrationID, false)
	if err != nil {
		return err
	}
	return store.DeletePrefix(ctx, prefix)
}

// PutObject uploads a static hosting asset with explicit Content-Type while
// computing its size and SHA-256 digest in a single streaming pass.
func (s *Store) PutObject(ctx context.Context, key, contentType string, content io.Reader) (int64, string, string, error) {
	if content == nil {
		return 0, "", "", fmt.Errorf("%w: object content reader is required", apperrors.ErrInvalidArgument)
	}
	normalizedKey := strings.Trim(strings.TrimSpace(key), "/")
	if normalizedKey == "" {
		return 0, "", "", fmt.Errorf("%w: object key is required", apperrors.ErrInvalidArgument)
	}
	normalizedType := strings.TrimSpace(contentType)
	if normalizedType == "" {
		normalizedType = "application/octet-stream"
	}

	hash := sha256.New()
	counter := &countingWriter{}
	body := io.TeeReader(content, io.MultiWriter(hash, counter))
	objectKey := s.objectKey(normalizedKey)

	out, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(s.config.Bucket),
		Key:         aws.String(objectKey),
		Body:        body,
		ContentType: aws.String(normalizedType),
	})
	if err != nil {
		return 0, "", "", errors.Join(fmt.Errorf("upload hosted object to object storage: %w", err), s.cleanup(ctx, objectKey))
	}
	sha256Hex := hex.EncodeToString(hash.Sum(nil))
	var rawETag string
	if out != nil {
		rawETag = aws.ToString(out.ETag)
	}
	return counter.n, sha256Hex, normalizeObjectETag(rawETag, sha256Hex), nil
}

// DeleteKeys deletes a bounded list of object keys relative to the store prefix.
func (s *Store) DeleteKeys(ctx context.Context, keys []string) error {
	resolved := s.uniqueObjectKeys(keys)
	if len(resolved) == 0 {
		return nil
	}
	if batchClient, ok := s.client.(s3BatchDeleter); ok {
		return s.deleteObjectBatch(ctx, batchClient, resolved)
	}
	var errs []error
	for _, objectKey := range resolved {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.config.Bucket),
			Key:    aws.String(objectKey),
		}); err != nil {
			errs = append(errs, fmt.Errorf("delete hosted object %s: %w", objectKey, err))
		}
	}
	return errors.Join(errs...)
}

// DeletePrefix deletes all objects stored under prefix when ListObjectsV2 is supported.
func (s *Store) DeletePrefix(ctx context.Context, prefix string) error {
	trimmed := strings.Trim(strings.TrimSpace(prefix), "/")
	if trimmed == "" {
		return fmt.Errorf("%w: delete prefix is required", apperrors.ErrInvalidArgument)
	}
	lister, ok := s.client.(s3PrefixLister)
	if !ok {
		return fmt.Errorf("%w: object storage client does not support prefix listing", apperrors.ErrUnsupportedOperation)
	}
	fullPrefix := s.objectKey(trimmed) + "/"
	var token *string
	for {
		page, err := lister.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.config.Bucket),
			Prefix:            aws.String(fullPrefix),
			ContinuationToken: token,
		})
		if err != nil {
			return fmt.Errorf("list hosted objects under %s: %w", fullPrefix, err)
		}
		keys := make([]string, 0, len(page.Contents))
		for _, obj := range page.Contents {
			if k := strings.TrimSpace(aws.ToString(obj.Key)); k != "" {
				keys = append(keys, k)
			}
		}
		if err := s.deleteExactObjectKeys(ctx, keys); err != nil {
			return err
		}
		if !aws.ToBool(page.IsTruncated) || page.NextContinuationToken == nil {
			return nil
		}
		token = page.NextContinuationToken
	}
}

func (s *Store) uniqueObjectKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	resolved := make([]string, 0, len(keys))
	for _, raw := range keys {
		trimmed := strings.Trim(strings.TrimSpace(raw), "/")
		if trimmed == "" {
			continue
		}
		objectKey := s.objectKey(trimmed)
		if _, exists := seen[objectKey]; exists {
			continue
		}
		seen[objectKey] = struct{}{}
		resolved = append(resolved, objectKey)
	}
	return resolved
}

func (s *Store) deleteExactObjectKeys(ctx context.Context, objectKeys []string) error {
	if len(objectKeys) == 0 {
		return nil
	}
	if batchClient, ok := s.client.(s3BatchDeleter); ok {
		return s.deleteObjectBatch(ctx, batchClient, objectKeys)
	}
	var errs []error
	for _, objectKey := range objectKeys {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.config.Bucket),
			Key:    aws.String(objectKey),
		}); err != nil {
			errs = append(errs, fmt.Errorf("delete hosted object %s: %w", objectKey, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Store) deleteObjectBatch(ctx context.Context, batchClient s3BatchDeleter, objectKeys []string) error {
	for start := 0; start < len(objectKeys); start += s3DeleteBatchSize {
		end := min(start+s3DeleteBatchSize, len(objectKeys))
		identifiers := make([]types.ObjectIdentifier, 0, end-start)
		for _, key := range objectKeys[start:end] {
			identifiers = append(identifiers, types.ObjectIdentifier{Key: aws.String(key)})
		}
		out, err := batchClient.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.config.Bucket),
			Delete: &types.Delete{Objects: identifiers, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("batch delete hosted objects: %w", err)
		}
		if out != nil && len(out.Errors) > 0 {
			first := out.Errors[0]
			return fmt.Errorf("batch delete hosted object %s: %s", aws.ToString(first.Key), aws.ToString(first.Message))
		}
	}
	return nil
}

func normalizeObjectETag(rawETag, sha256Hex string) string {
	trimmed := strings.TrimSpace(rawETag)
	if trimmed == "" {
		return `"sha256:` + sha256Hex + `"`
	}
	if strings.HasPrefix(trimmed, `W/"`) && strings.HasSuffix(trimmed, `"`) {
		return trimmed
	}
	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) && len(trimmed) >= 2 {
		return trimmed
	}
	return `"` + strings.Trim(trimmed, `"`) + `"`
}
