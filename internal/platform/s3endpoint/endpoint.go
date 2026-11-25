// Package s3endpoint configures AWS SDK v2 S3 clients for bucket-specific
// HTTPS endpoints such as object-storage custom domains.
package s3endpoint

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

// RejectRedirect keeps bucket-endpoint traffic on the exact configured origin.
func RejectRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

var (
	errInvalidURL    = errors.New("bucket endpoint is not a valid URL")
	errHTTPSRequired = errors.New("bucket endpoint must use HTTPS")
	errHostRequired  = errors.New("bucket endpoint must include a valid host")
	errUserInfo      = errors.New("bucket endpoint must not include user information")
	errPath          = errors.New("bucket endpoint must not include a path")
	errQuery         = errors.New("bucket endpoint must not include a query")
	errFragment      = errors.New("bucket endpoint must not include a fragment")
	errPathStyle     = errors.New("bucket endpoint does not support path-style addressing")
	errInsecure      = errors.New("bucket endpoint does not allow insecure TLS")
	errBucket        = errors.New("S3 request bucket does not match the bucket endpoint")
)

// ValidateBucketEndpoint verifies that endpoint can safely serve one S3 bucket
// through virtual-hosted addressing.
func ValidateBucketEndpoint(endpoint string, pathStyle, insecure bool) error {
	if pathStyle {
		return errPathStyle
	}
	if insecure {
		return errInsecure
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return errInvalidURL
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return errHTTPSRequired
	}
	if parsed.Host == "" || !validHost(parsed.Hostname()) || strings.HasSuffix(parsed.Host, ":") {
		return errHostRequired
	}
	if parsed.User != nil {
		return errUserInfo
	}
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return errPath
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return errQuery
	}
	if parsed.Fragment != "" || strings.Contains(endpoint, "#") {
		return errFragment
	}
	return nil
}

// ConfigureBucketEndpoint opts an S3 client into treating BaseEndpoint as the
// exact address for bucket. It does nothing without a valid BaseEndpoint.
func ConfigureBucketEndpoint(opts *s3.Options, bucket string) {
	if opts == nil || opts.BaseEndpoint == nil || opts.UsePathStyle {
		return
	}
	if ValidateBucketEndpoint(*opts.BaseEndpoint, false, false) != nil {
		return
	}
	opts.EndpointResolverV2 = bucketEndpointResolver{
		bucket: bucket,
		next:   s3.NewDefaultEndpointResolverV2(),
	}
}

type bucketEndpointResolver struct {
	bucket string
	next   s3.EndpointResolverV2
}

func (r bucketEndpointResolver) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	if params.Bucket == nil || *params.Bucket != r.bucket {
		return smithyendpoints.Endpoint{}, errBucket
	}
	params.Bucket = nil
	return r.next.ResolveEndpoint(ctx, params)
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
