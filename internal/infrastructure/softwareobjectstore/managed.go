package softwareobjectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// PrepareManaged freezes one explicit configured integration for a content
// operation. It neither connects to storage nor adopts installer metadata.
type ManagedSnapshot struct {
	*Store
	revision int64
}

func (p *Provider) PrepareManaged(ctx context.Context, id string) (*ManagedSnapshot, error) {
	if p == nil || p.resolver == nil || id == "" {
		return nil, apperrors.ErrInvalidArgument
	}
	item, credentials, err := p.resolver.ResolveStorageConnection(ctx, id, true)
	if err != nil {
		return nil, err
	}
	store, err := p.build(item, credentials)
	if err != nil {
		return nil, err
	}
	// Managed content may contain protected plaintext. Keep legacy installer
	// configuration compatible while requiring encrypted managed transport.
	if strings.HasPrefix(store.config.Endpoint, "http://") {
		return nil, apperrors.ErrInvalidArgument
	}
	return &ManagedSnapshot{Store: store, revision: item.Version}, nil
}
func (s *ManagedSnapshot) SnapshotIdentity() string {
	sum := sha256.Sum256([]byte(s.ManagedIdentity() + "/" + strconv.FormatInt(s.revision, 10)))
	return hex.EncodeToString(sum[:])
}
func (s *Store) ManagedIdentity() string {
	raw, _ := json.Marshal([]any{"s3", s.config.IntegrationID, s.config.Endpoint, s.config.Bucket, s.config.Region, s.config.Prefix, s.config.PathStyle, s.config.Insecure})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Managed content uses a bounded create-only PUT. Installer cleanup/retry rules
// must not delete an object after an uncertain content-transfer response.
func (s *Store) PutManaged(ctx context.Context, key string, content []byte) error {
	if len(content) > 256<<10 {
		return apperrors.ErrInvalidArgument
	}
	client, ok := s.client.(interface {
		PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	})
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(s.objectKey(key)), Body: bytes.NewReader(content), ContentType: aws.String("application/octet-stream"), ContentLength: aws.Int64(int64(len(content))), IfNoneMatch: aws.String("*")})
	if err != nil {
		return errors.Join(apperrors.ErrServiceUnavailable, err)
	}
	return nil
}
