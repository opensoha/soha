package softwareobjectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/netguard"
)

type memoryS3 struct {
	objects map[string][]byte
	tested  bool
}

func (m *memoryS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	m.tested = true
	return &s3.HeadBucketOutput{}, nil
}

func (m *memoryS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(m.objects[aws.ToString(input.Key)]))}, nil
}

func (m *memoryS3) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(m.objects, aws.ToString(input.Key))
	return &s3.DeleteObjectOutput{}, nil
}

type memoryUploader struct{ objects map[string][]byte }

func (m memoryUploader) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	m.objects[aws.ToString(input.Key)] = payload
	return &transfermanager.UploadObjectOutput{Key: input.Key, ContentLength: aws.Int64(int64(len(payload)))}, nil
}

type staticResolver struct {
	item        domain.Integration
	credentials map[string]string
}

func (r staticResolver) ResolveStorageConnection(context.Context, string, bool) (domain.Integration, map[string]string, error) {
	return r.item, r.credentials, nil
}

func TestParseConnectionAndObjectLifecycle(t *testing.T) {
	item := domain.Integration{
		ID: "storage-1", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "endpoint", Value: "https://minio.example.com/"},
			{Key: "bucket", Value: "soha-software"},
			{Key: "region", Value: "us-east-1"},
			{Key: "path_style", Value: "true"},
			{Key: "prefix", Value: "/managed/"},
		},
	}
	config, err := parseConnection(item, map[string]string{"access_key_id": "access", "secret_access_key": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if config.Endpoint != "https://minio.example.com" || !config.PathStyle || config.Prefix != "managed" {
		t.Fatalf("parsed config = %#v", config)
	}

	objects := map[string][]byte{}
	client := &memoryS3{objects: objects}
	store := newStore(config, client, memoryUploader{objects: objects})
	payload := []byte("signed installer payload")
	size, digest, err := store.Put(t.Context(), "packages/pkg-1", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(payload)
	if size != int64(len(payload)) || digest != hex.EncodeToString(wantDigest[:]) || !bytes.Equal(objects["managed/packages/pkg-1"], payload) {
		t.Fatalf("put result size=%d digest=%q objects=%#v", size, digest, objects)
	}
	reader, err := store.Open(t.Context(), "packages/pkg-1")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("open payload=%q err=%v", got, readErr)
	}
	if err := store.TestConnection(t.Context()); err != nil || !client.tested {
		t.Fatalf("test connection called=%v err=%v", client.tested, err)
	}
	if err := store.Delete(t.Context(), "packages/pkg-1"); err != nil || len(objects) != 0 {
		t.Fatalf("delete objects=%#v err=%v", objects, err)
	}
}

func TestParseConnectionRejectsSecretOrInsecureConfiguration(t *testing.T) {
	item := domain.Integration{
		ID: "storage-1", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "endpoint", Value: "http://minio.example.com"},
			{Key: "bucket", Value: "soha-software"},
			{Key: "region", Value: "us-east-1"},
		},
	}
	if _, err := parseConnection(item, map[string]string{"access_key_id": "access", "secret_access_key": "secret"}); err == nil {
		t.Fatal("expected insecure endpoint to be rejected")
	}
	item.Configuration = append(item.Configuration, sohaapi.SystemIntegrationConfigurationField{Key: "insecure", Value: "true"})
	if _, err := parseConnection(item, map[string]string{"access_key_id": "access"}); err == nil {
		t.Fatal("expected missing secret_access_key to be rejected")
	}
	item.Configuration[0].Value = "https://127.0.0.1"
	if _, err := parseConnection(item, map[string]string{"access_key_id": "access", "secret_access_key": "secret"}); err == nil {
		t.Fatal("expected private endpoint to require allow_private")
	}
	item.Configuration = append(item.Configuration, sohaapi.SystemIntegrationConfigurationField{Key: "allow_private", Value: "true"})
	if _, err := parseConnection(item, map[string]string{"access_key_id": "access", "secret_access_key": "secret"}); err != nil {
		t.Fatalf("explicit private endpoint should be accepted: %v", err)
	}
}

func TestProviderResolvesActiveAndHistoricalConnectionsForObjectOperations(t *testing.T) {
	item := domain.Integration{
		ID: "storage-1", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3, HealthStatus: domain.HealthHealthy,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "bucket", Value: "soha-software"}, {Key: "region", Value: "us-east-1"},
		},
	}
	objects := map[string][]byte{}
	client := &memoryS3{objects: objects}
	provider := NewProvider(staticResolver{item: item, credentials: map[string]string{"access_key_id": "access", "secret_access_key": "secret"}})
	provider.build = func(_ domain.Integration, _ map[string]string) (*Store, error) {
		config := Config{IntegrationID: "storage-1", Bucket: "soha-software", Region: "us-east-1", HealthStatus: domain.HealthHealthy}
		return newStore(config, client, memoryUploader{objects: objects}), nil
	}

	backend, err := provider.Active(t.Context(), "storage-1")
	if err != nil || backend.IntegrationID != "storage-1" || backend.HealthStatus != domain.HealthHealthy {
		t.Fatalf("active backend=%#v err=%v", backend, err)
	}
	if _, _, err := provider.Put(t.Context(), "storage-1", "pkg", bytes.NewBufferString("payload")); err != nil {
		t.Fatal(err)
	}
	reader, err := provider.Open(t.Context(), "storage-1", "pkg")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(got) != "payload" {
		t.Fatalf("payload=%q", got)
	}
	if err := provider.Delete(t.Context(), "storage-1", "pkg"); err != nil || len(objects) != 0 {
		t.Fatalf("delete objects=%#v err=%v", objects, err)
	}
	if _, err := NewProvider(nil).Active(t.Context(), ""); err == nil {
		t.Fatal("nil resolver should fail closed")
	}
}

func TestFactoryBuildsConfiguredS3TesterWithoutNetworkAccess(t *testing.T) {
	item := domain.Integration{
		ID: "storage-1", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "endpoint", Value: "https://minio.example.com"},
			{Key: "bucket", Value: "soha-software"},
			{Key: "region", Value: "us-east-1"},
			{Key: "path_style", Value: "true"},
		},
	}
	tester, err := (Factory{}).Build(item, map[string]string{"access_key_id": "access", "secret_access_key": "secret"})
	if err != nil || tester == nil {
		t.Fatalf("factory tester=%#v err=%v", tester, err)
	}
	if got := (Factory{}).Capabilities(); len(got) != 3 || got[2] != "object.delete" {
		t.Fatalf("capabilities=%#v", got)
	}
}

func TestStoreRejectsEmptyPayloadAndInvalidConnectionFields(t *testing.T) {
	objects := map[string][]byte{}
	client := &memoryS3{objects: objects}
	store := newStore(Config{Bucket: "bucket", Region: "us-east-1"}, client, memoryUploader{objects: objects})
	if _, _, err := store.Put(t.Context(), "empty", bytes.NewReader(nil)); err == nil || len(objects) != 0 {
		t.Fatalf("empty payload error=%v objects=%#v", err, objects)
	}
	if _, _, err := store.Put(t.Context(), "nil", nil); err == nil {
		t.Fatal("nil payload should fail")
	}

	base := domain.Integration{
		ID: "storage-1", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "bucket", Value: "bucket"}, {Key: "region", Value: "us-east-1"}},
	}
	cases := []domain.Integration{
		{ID: "wrong", Category: domain.CategorySourceControl, ProviderType: domain.ProviderGitLab},
		{ID: "bad-bool", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3, Configuration: append(base.Configuration, sohaapi.SystemIntegrationConfigurationField{Key: "path_style", Value: "maybe"})},
		{ID: "missing", Category: domain.CategoryStorage, ProviderType: domain.ProviderS3},
	}
	for _, item := range cases {
		if _, err := parseConnection(item, map[string]string{"access_key_id": "access", "secret_access_key": "secret"}); err == nil {
			t.Fatalf("connection %s should fail", item.ID)
		}
	}
}

func TestStorageHTTPClientBlocksPrivateResolutionUnlessExplicitlyAllowed(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1"} {
		if !netguard.BlockedOutboundIP(net.ParseIP(value)) {
			t.Fatalf("address %s should be blocked", value)
		}
	}
	if netguard.BlockedOutboundIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address should be allowed")
	}
	if _, err := dialPublicAddress(t.Context(), "tcp", "127.0.0.1:9000"); err == nil {
		t.Fatal("private literal endpoint should not be dialed")
	}
	if storageHTTPClient(false).Transport == nil || storageHTTPClient(true).Transport == nil {
		t.Fatal("storage clients require explicit transports")
	}
}
