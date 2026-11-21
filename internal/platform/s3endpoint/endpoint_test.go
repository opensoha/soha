package s3endpoint

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type recordingHTTPClient struct {
	calls         int
	host          string
	path          string
	contentType   string
	authorization string
}

func (c *recordingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.calls++
	c.host = req.URL.Host
	c.path = req.URL.EscapedPath()
	c.contentType = req.Header.Get("Content-Type")
	c.authorization = req.Header.Get("Authorization")
	body := ""
	if req.Method == http.MethodGet {
		body = "payload"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func TestValidateBucketEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		endpoint  string
		pathStyle bool
		insecure  bool
		secret    string
		wantErr   bool
	}{
		{name: "valid", endpoint: "https://assets.example.com"},
		{name: "valid root path", endpoint: "https://assets.example.com/"},
		{name: "http", endpoint: "http://assets.example.com", wantErr: true},
		{name: "missing host", endpoint: "https:///assets", wantErr: true},
		{name: "invalid host", endpoint: "https://bad_host.example.com", wantErr: true},
		{name: "userinfo", endpoint: "https://user:password@assets.example.com", secret: "password", wantErr: true},
		{name: "path", endpoint: "https://assets.example.com/assets", wantErr: true},
		{name: "query", endpoint: "https://assets.example.com?token=secret", secret: "secret", wantErr: true},
		{name: "fragment", endpoint: "https://assets.example.com#secret", secret: "secret", wantErr: true},
		{name: "path style", endpoint: "https://assets.example.com", pathStyle: true, wantErr: true},
		{name: "insecure", endpoint: "https://assets.example.com", insecure: true, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBucketEndpoint(test.endpoint, test.pathStyle, test.insecure)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateBucketEndpoint() error = %v, wantErr %v", err, test.wantErr)
			}
			if err != nil && test.secret != "" && strings.Contains(err.Error(), test.secret) {
				t.Fatalf("error leaked endpoint secret: %v", err)
			}
		})
	}
}

func TestRegionalVirtualHostEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		endpoint  string
		bucket    string
		region    string
		wantHost  string
		pathStyle bool
		wantPath  string
	}{
		{
			name:     "OSS regional",
			endpoint: "https://s3.oss-ap-southeast-1.aliyuncs.com",
			bucket:   "soha-demo",
			region:   "ap-southeast-1",
			wantHost: "soha-demo.s3.oss-ap-southeast-1.aliyuncs.com",
			wantPath: "/dir/a%20b.txt",
		},
		{
			name:     "COS regional",
			endpoint: "https://cos.ap-guangzhou.myqcloud.com",
			bucket:   "soha-1250000000",
			region:   "ap-guangzhou",
			wantHost: "soha-1250000000.cos.ap-guangzhou.myqcloud.com",
			wantPath: "/dir/a%20b.txt",
		},
		{
			name:      "MinIO path style",
			endpoint:  "http://minio.example.test:9000",
			bucket:    "soha-demo",
			region:    "us-east-1",
			wantHost:  "minio.example.test:9000",
			pathStyle: true,
			wantPath:  "/soha-demo/dir/a%20b.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, httpClient := newTestClient(test.endpoint, test.bucket, test.region, func(opts *s3.Options) {
				opts.UsePathStyle = test.pathStyle
			})
			_, err := client.GetObject(t.Context(), &s3.GetObjectInput{
				Bucket: aws.String(test.bucket),
				Key:    aws.String("dir/a b.txt"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if httpClient.host != test.wantHost {
				t.Fatalf("host = %q, want %q", httpClient.host, test.wantHost)
			}
			if httpClient.path != test.wantPath {
				t.Fatalf("path = %q, want %q", httpClient.path, test.wantPath)
			}
			if !strings.Contains(httpClient.authorization, "/"+test.region+"/s3/aws4_request") {
				t.Fatalf("authorization = %q, want region %q", httpClient.authorization, test.region)
			}
		})
	}
}

func TestConfigureBucketEndpoint(t *testing.T) {
	const (
		endpoint = "https://assets.example.com"
		bucket   = "soha-assets"
		region   = "cn-hangzhou"
		key      = "dir/a b.txt"
	)

	for _, operation := range []string{"head-bucket", "head-object", "get", "put", "delete"} {
		t.Run(operation, func(t *testing.T) {
			client, httpClient := newTestClient(endpoint, bucket, region, func(opts *s3.Options) {
				ConfigureBucketEndpoint(opts, bucket)
			})
			var err error
			wantPath := "/dir/a%20b.txt"
			switch operation {
			case "head-bucket":
				_, err = client.HeadBucket(t.Context(), &s3.HeadBucketInput{Bucket: aws.String(bucket)})
				wantPath = "/"
			case "head-object":
				_, err = client.HeadObject(t.Context(), &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			case "get":
				_, err = client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			case "put":
				_, err = client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader("payload"), ContentType: aws.String("text/plain")})
			case "delete":
				_, err = client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			}
			if err != nil {
				t.Fatal(err)
			}
			if httpClient.calls != 1 {
				t.Fatalf("calls = %d, want 1", httpClient.calls)
			}
			if httpClient.host != "assets.example.com" {
				t.Fatalf("host = %q, want fixed bucket domain", httpClient.host)
			}
			if httpClient.path != wantPath {
				t.Fatalf("path = %q, want %q", httpClient.path, wantPath)
			}
			if operation == "put" && httpClient.contentType != "text/plain" {
				t.Fatalf("content type = %q, want text/plain", httpClient.contentType)
			}
			if !strings.Contains(httpClient.authorization, "/"+region+"/s3/aws4_request") {
				t.Fatalf("authorization = %q, want region %q", httpClient.authorization, region)
			}
		})
	}
}

func TestConfigureBucketEndpointRejectsOtherBucket(t *testing.T) {
	client, httpClient := newTestClient("https://assets.example.com", "soha-assets", "cn-hangzhou", func(opts *s3.Options) {
		ConfigureBucketEndpoint(opts, "soha-assets")
	})
	_, err := client.GetObject(t.Context(), &s3.GetObjectInput{
		Bucket: aws.String("other-bucket"),
		Key:    aws.String("index.html"),
	})
	if err == nil {
		t.Fatal("expected wrong bucket to be rejected")
	}
	if httpClient.calls != 0 {
		t.Fatalf("calls = %d, want 0", httpClient.calls)
	}

	_, err = client.ListBuckets(t.Context(), &s3.ListBucketsInput{})
	if err == nil {
		t.Fatal("expected missing bucket to be rejected")
	}
	if httpClient.calls != 0 {
		t.Fatalf("calls = %d, want 0", httpClient.calls)
	}
}

func TestConfigureBucketEndpointRequiresOptIn(t *testing.T) {
	opts := &s3.Options{}
	ConfigureBucketEndpoint(opts, "soha-assets")
	if opts.EndpointResolverV2 != nil {
		t.Fatal("resolver installed without BaseEndpoint")
	}
}

func TestBucketEndpointRejectsRedirect(t *testing.T) {
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://invalid.invalid/other-object", http.StatusTemporaryRedirect)
	}))
	defer endpoint.Close()

	client := endpoint.Client()
	client.CheckRedirect = RejectRedirect
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint.URL+"/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307 from configured endpoint", resp.StatusCode)
	}
}

func newTestClient(endpoint, bucket, region string, configure func(*s3.Options)) (*s3.Client, *recordingHTTPClient) {
	httpClient := &recordingHTTPClient{}
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider("access-key", "secret-key", ""),
		HTTPClient:  httpClient,
		Retryer:     func() aws.Retryer { return aws.NopRetryer{} },
	}
	client := s3.NewFromConfig(cfg, func(opts *s3.Options) {
		opts.BaseEndpoint = aws.String(endpoint)
		configure(opts)
	})
	return client, httpClient
}
