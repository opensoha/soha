package softwareobjectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	appsoftware "github.com/opensoha/soha/internal/application/software"
	appsystemintegration "github.com/opensoha/soha/internal/application/systemintegration"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
)

type ConnectionResolver interface {
	ResolveStorageConnection(context.Context, string, bool) (domain.Integration, map[string]string, error)
}

type Config struct {
	IntegrationID string
	Endpoint      string
	Bucket        string
	Region        string
	Prefix        string
	PathStyle     bool
	Insecure      bool
	AllowPrivate  bool
	AccessKeyID   string
	SecretKey     string
	SessionToken  string
	HealthStatus  string
	LastCheckedAt *time.Time
}

type s3Operations interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type objectUploader interface {
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

type Store struct {
	config   Config
	client   s3Operations
	uploader objectUploader
}

type Factory struct{}

func (Factory) Build(item domain.Integration, credentials map[string]string) (appsystemintegration.ConnectionTester, error) {
	return buildStore(item, credentials)
}

func (Factory) Capabilities() []string {
	return []string{"object.get", "object.put", "object.delete"}
}

type Provider struct {
	resolver ConnectionResolver
	build    func(domain.Integration, map[string]string) (*Store, error)
}

func NewProvider(resolver ConnectionResolver) *Provider {
	return &Provider{resolver: resolver, build: buildStore}
}

func (p *Provider) Active(ctx context.Context, integrationID string) (appsoftware.StorageBackend, error) {
	store, err := p.resolve(ctx, integrationID, true)
	if err != nil {
		return appsoftware.StorageBackend{}, err
	}
	return store.backend(), nil
}

func (p *Provider) Put(ctx context.Context, integrationID, key string, content io.Reader) (int64, string, error) {
	store, err := p.resolve(ctx, integrationID, true)
	if err != nil {
		return 0, "", err
	}
	return store.Put(ctx, key, content)
}

func (p *Provider) Open(ctx context.Context, integrationID, key string) (io.ReadCloser, error) {
	store, err := p.resolve(ctx, integrationID, false)
	if err != nil {
		return nil, err
	}
	return store.Open(ctx, key)
}

func (p *Provider) Delete(ctx context.Context, integrationID, key string) error {
	store, err := p.resolve(ctx, integrationID, false)
	if err != nil {
		return err
	}
	return store.Delete(ctx, key)
}

func (p *Provider) resolve(ctx context.Context, integrationID string, requireEnabled bool) (*Store, error) {
	if p == nil || p.resolver == nil {
		return nil, fmt.Errorf("%w: software object storage is unavailable", apperrors.ErrServiceUnavailable)
	}
	item, credentials, err := p.resolver.ResolveStorageConnection(ctx, integrationID, requireEnabled)
	if err != nil {
		return nil, err
	}
	return p.build(item, credentials)
}

func buildStore(item domain.Integration, values map[string]string) (*Store, error) {
	config, err := parseConnection(item, values)
	if err != nil {
		return nil, err
	}
	awsConfig := aws.Config{
		Region:                     config.Region,
		Credentials:                aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretKey, config.SessionToken)),
		HTTPClient:                 storageHTTPClient(config.AllowPrivate),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.UsePathStyle = config.PathStyle
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(config.Endpoint)
		}
	})
	return newStore(config, client, transfermanager.New(client)), nil
}

func newStore(config Config, client s3Operations, uploader objectUploader) *Store {
	return &Store{config: config, client: client, uploader: uploader}
}

func parseConnection(item domain.Integration, credentials map[string]string) (Config, error) {
	if item.Category != domain.CategoryStorage || item.ProviderType != domain.ProviderS3 {
		return Config{}, fmt.Errorf("%w: unsupported software object storage integration", apperrors.ErrInvalidArgument)
	}
	fields := make(map[string]string, len(item.Configuration))
	for _, field := range item.Configuration {
		fields[field.Key] = strings.TrimSpace(field.Value)
	}
	config := Config{
		IntegrationID: item.ID,
		Endpoint:      strings.TrimRight(fields["endpoint"], "/"),
		Bucket:        fields["bucket"], Region: fields["region"], Prefix: strings.Trim(fields["prefix"], "/"),
		AccessKeyID: credentials["access_key_id"], SecretKey: credentials["secret_access_key"], SessionToken: credentials["session_token"],
		HealthStatus: item.HealthStatus, LastCheckedAt: item.LastCheckedAt,
	}
	if err := parseConnectionFlags(&config, fields); err != nil {
		return Config{}, err
	}
	if err := validateConnectionConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func parseConnectionFlags(config *Config, fields map[string]string) error {
	var err error
	if config.PathStyle, err = optionalBool(fields["path_style"]); err != nil {
		return fmt.Errorf("%w: invalid s3 path_style", apperrors.ErrInvalidArgument)
	}
	if config.Insecure, err = optionalBool(fields["insecure"]); err != nil {
		return fmt.Errorf("%w: invalid s3 insecure", apperrors.ErrInvalidArgument)
	}
	if config.AllowPrivate, err = optionalBool(fields["allow_private"]); err != nil {
		return fmt.Errorf("%w: invalid s3 allow_private", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateConnectionConfig(config Config) error {
	if config.Endpoint != "" {
		parsed, err := url.Parse(config.Endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Scheme == "http" && !config.Insecure {
			return fmt.Errorf("%w: invalid s3 endpoint", apperrors.ErrInvalidArgument)
		}
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && netguard.BlockedOutboundIP(ip) && !config.AllowPrivate {
			return fmt.Errorf("%w: private s3 endpoint requires allow_private=true", apperrors.ErrInvalidArgument)
		}
	}
	if config.Bucket == "" || config.Region == "" || config.AccessKeyID == "" || config.SecretKey == "" {
		return fmt.Errorf("%w: incomplete s3 storage configuration", apperrors.ErrInvalidArgument)
	}
	return nil
}

func optionalBool(value string) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}

func (s *Store) TestConnection(ctx context.Context) error {
	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := s.client.HeadBucket(testCtx, &s3.HeadBucketInput{Bucket: aws.String(s.config.Bucket)})
	if err != nil {
		return fmt.Errorf("test s3 bucket access: %w", err)
	}
	return nil
}

func (s *Store) Put(ctx context.Context, key string, content io.Reader) (int64, string, error) {
	if content == nil {
		return 0, "", fmt.Errorf("%w: software package content is required", apperrors.ErrInvalidArgument)
	}
	hash := sha256.New()
	counter := &countingWriter{}
	body := io.TeeReader(io.LimitReader(content, appsoftware.MaxPackageBytes+1), io.MultiWriter(hash, counter))
	objectKey := s.objectKey(key)
	if _, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(s.config.Bucket), Key: aws.String(objectKey), Body: body,
		ContentType: aws.String("application/octet-stream"),
	}); err != nil {
		return 0, "", errors.Join(fmt.Errorf("upload software package to object storage: %w", err), s.cleanup(ctx, objectKey))
	}
	if counter.n < 1 || counter.n > appsoftware.MaxPackageBytes {
		return 0, "", errors.Join(fmt.Errorf("%w: installer must be between 1 and %d bytes", apperrors.ErrInvalidArgument, appsoftware.MaxPackageBytes), s.cleanup(ctx, objectKey))
	}
	return counter.n, hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(s.objectKey(key))})
	if err != nil {
		return nil, fmt.Errorf("open software package from object storage: %w", err)
	}
	return output.Body, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(s.objectKey(key))})
	if err != nil {
		return fmt.Errorf("delete software package from object storage: %w", err)
	}
	return nil
}

func (s *Store) objectKey(key string) string {
	return strings.TrimLeft(path.Join(s.config.Prefix, strings.TrimLeft(key, "/")), "/")
}

func (s *Store) cleanup(ctx context.Context, objectKey string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, err := s.client.DeleteObject(cleanupCtx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(objectKey)})
	if err != nil {
		return fmt.Errorf("cleanup software package object: %w", err)
	}
	return nil
}

func storageHTTPClient(allowPrivate bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if !allowPrivate {
		transport.DialContext = dialPublicAddress
	}
	return &http.Client{Transport: transport}
}

func dialPublicAddress(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("s3 endpoint host did not resolve")
	}
	for _, address := range addresses {
		if netguard.BlockedOutboundIP(address.IP) {
			return nil, fmt.Errorf("s3 endpoint resolved to a private or reserved address")
		}
	}
	dialer := net.Dialer{}
	return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func (s *Store) backend() appsoftware.StorageBackend {
	return appsoftware.StorageBackend{
		IntegrationID: s.config.IntegrationID, ProviderType: domain.ProviderS3, Endpoint: s.config.Endpoint,
		Bucket: s.config.Bucket, Region: s.config.Region, HealthStatus: s.config.HealthStatus, LastCheckedAt: s.config.LastCheckedAt,
	}
}

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}
