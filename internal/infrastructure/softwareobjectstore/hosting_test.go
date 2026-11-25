package softwareobjectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
)

type hostingMemoryS3 struct {
	memoryS3
	batchDeleted []string
}

func (m *hostingMemoryS3) DeleteObjects(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	if input.Delete != nil {
		for _, item := range input.Delete.Objects {
			key := aws.ToString(item.Key)
			delete(m.objects, key)
			m.batchDeleted = append(m.batchDeleted, key)
		}
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (m *hostingMemoryS3) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(input.Prefix)
	var contents []types.Object
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			contents = append(contents, types.Object{Key: aws.String(key)})
		}
	}
	return &s3.ListObjectsV2Output{Contents: contents}, nil
}

type hostingMemoryUploader struct {
	objects     map[string][]byte
	contentType map[string]string
	etag        string
}

func (m *hostingMemoryUploader) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	key := aws.ToString(input.Key)
	m.objects[key] = payload
	if m.contentType != nil {
		m.contentType[key] = aws.ToString(input.ContentType)
	}
	out := &transfermanager.UploadObjectOutput{Key: input.Key, ContentLength: aws.Int64(int64(len(payload)))}
	if m.etag != "" {
		out.ETag = aws.String(m.etag)
	}
	return out, nil
}

func TestProviderPutObjectPreservesContentTypeFallbackETagAndZeroByteFiles(t *testing.T) {
	item := domain.Integration{
		ID: "storage-1", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3, HealthStatus: domain.HealthHealthy,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "bucket", Value: "soha-apps"}, {Key: "region", Value: "us-east-1"}, {Key: "prefix", Value: "tenant-a"},
		},
	}
	objects := map[string][]byte{}
	typesByKey := map[string]string{}
	client := &hostingMemoryS3{memoryS3: memoryS3{objects: objects}}
	uploader := &hostingMemoryUploader{objects: objects, contentType: typesByKey}

	provider := NewProvider(staticResolver{item: item, credentials: map[string]string{"access_key_id": "access", "secret_access_key": "secret"}})
	provider.build = func(_ domain.Integration, _ map[string]string) (*Store, error) {
		return newStore(Config{IntegrationID: "storage-1", Bucket: "soha-apps", Region: "us-east-1", Prefix: "tenant-a"}, client, uploader), nil
	}

	body := []byte("<!doctype html><html><body>ok</body></html>")
	size, digest, etag, err := provider.PutObject(t.Context(), "storage-1", "/app-hosting/app_1/rev_1/index.html", "text/html; charset=utf-8", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}
	sum := sha256.Sum256(body)
	wantDigest := hex.EncodeToString(sum[:])
	if size != int64(len(body)) || digest != wantDigest || etag != `"sha256:`+wantDigest+`"` {
		t.Fatalf("got size=%d digest=%q etag=%q", size, digest, etag)
	}
	storedKey := "tenant-a/app-hosting/app_1/rev_1/index.html"
	if typesByKey[storedKey] != "text/html; charset=utf-8" {
		t.Fatalf("contentType = %q", typesByKey[storedKey])
	}

	uploader.etag = "abc123etag"
	emptySize, _, quotedETag, err := provider.PutObject(t.Context(), "storage-1", "app-hosting/app_1/rev_1/.keep", "", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("zero-byte PutObject failed: %v", err)
	}
	if emptySize != 0 || quotedETag != `"abc123etag"` {
		t.Fatalf("zero-byte result size=%d etag=%q", emptySize, quotedETag)
	}

	if err := provider.DeleteKeys(t.Context(), "storage-1", []string{
		"app-hosting/app_1/rev_1/index.html",
		"app-hosting/app_1/rev_1/.keep",
		"app-hosting/app_1/rev_1/.keep",
	}); err != nil {
		t.Fatalf("DeleteKeys failed: %v", err)
	}
	if len(objects) != 0 || len(client.batchDeleted) != 2 {
		t.Fatalf("remaining objects=%#v batchDeleted=%v", objects, client.batchDeleted)
	}
}

func TestStoreDeleteKeysFallbackAndDeletePrefix(t *testing.T) {
	objects := map[string][]byte{
		"pfx/app-hosting/app_1/rev_1/index.html": []byte("a"),
		"pfx/app-hosting/app_1/rev_1/app.js":     []byte("b"),
		"pfx/app-hosting/app_1/rev_2/index.html": []byte("c"),
	}
	// Fallback single-key DeleteObject path when DeleteObjects is not implemented.
	basicStore := newStore(Config{Bucket: "b", Region: "us-east-1", Prefix: "pfx"}, &memoryS3{objects: objects}, memoryUploader{objects: objects})
	if err := basicStore.DeleteKeys(t.Context(), []string{"app-hosting/app_1/rev_2/index.html"}); err != nil {
		t.Fatalf("fallback DeleteKeys: %v", err)
	}
	if _, exists := objects["pfx/app-hosting/app_1/rev_2/index.html"]; exists {
		t.Fatal("expected rev_2 key to be deleted")
	}

	// Prefix delete path when ListObjectsV2 + DeleteObjects are implemented.
	batchClient := &hostingMemoryS3{memoryS3: memoryS3{objects: objects}}
	prefixStore := newStore(Config{Bucket: "b", Region: "us-east-1", Prefix: "pfx"}, batchClient, memoryUploader{objects: objects})
	if err := prefixStore.DeletePrefix(t.Context(), "app-hosting/app_1/rev_1"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("expected all rev_1 objects deleted, got %#v", objects)
	}
}
