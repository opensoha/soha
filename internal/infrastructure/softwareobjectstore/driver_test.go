package softwareobjectstore

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDriverBinding(endpoint string) storagedriver.Binding {
	return storagedriver.Binding{
		ID: "binding-1", Container: "bucket", Prefix: "scope/",
		Settings: json.RawMessage(fmt.Sprintf(
			`{"endpoint":%q,"region":"us-east-1","path_style":"true","allow_private":"true","insecure":"true"}`,
			endpoint,
		)),
		Credentials: map[string]string{
			"access_key_id": "test-access", "secret_access_key": "test-secret", "session_token": "test-token",
		},
	}
}

func TestS3DriverExactKeysAndOpen(t *testing.T) {
	const (
		key         = "Group/a +%?#/你好 "
		payload     = "0123456789"
		etag        = `"committed"`
		contentType = "text/plain; charset=utf-8"
	)
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "Credential=test-access/")
		assert.Equal(t, "test-token", r.Header.Get("X-Amz-Security-Token"))
		assert.Equal(t, "/bucket/scope/"+key, r.URL.Path)
		w.Header().Set("ETag", etag)
		switch r.Method {
		case http.MethodPut:
			assert.Equal(t, contentType, r.Header.Get("Content-Type"))
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, payload, string(body))
		case http.MethodGet, http.MethodHead:
			w.Header().Set("Content-Type", contentType)
			switch r.Header.Get("Range") {
			case "bytes=7-9":
				assert.Equal(t, etag, r.Header.Get("If-Match"))
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Content-Range", "bytes 7-9/10")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = fmt.Fprint(w, payload[7:])
			default:
				assert.Empty(t, r.Header.Get("Range"))
				w.Header().Set("Content-Length", "10")
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprint(w, payload)
				}
			}
		case http.MethodDelete:
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request method %s", r.Method)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	binding := testDriverBinding(server.URL)
	driver := NewS3Driver()
	put, err := driver.PutObject(t.Context(), storagedriver.PutObjectRequest{
		Binding: binding, Key: key, ContentType: contentType,
	}, strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, storagedriver.PutObjectResult{
		Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(payload))), ETag: etag,
	}, put)

	full, err := driver.OpenObject(t.Context(), storagedriver.OpenObjectRequest{Binding: binding, Key: key})
	require.NoError(t, err)
	require.Equal(t, contentType, full.ContentType)
	require.EqualValues(t, len(payload), full.ContentLength)
	require.Nil(t, full.ContentRange)
	requireDriverBody(t, full.Body, payload)

	notModified, err := driver.OpenObject(t.Context(), storagedriver.OpenObjectRequest{
		Binding: binding, Key: key, Range: "bytes=999-", IfNoneMatch: `"other", W/"committed"`,
	})
	require.NoError(t, err)
	require.Equal(t, storagedriver.OpenObjectResult{ETag: etag, NotModified: true}, notModified)

	partial, err := driver.OpenObject(t.Context(), storagedriver.OpenObjectRequest{
		Binding: binding, Key: key, Range: "bytes=-3",
	})
	require.NoError(t, err)
	require.Equal(t, &storagedriver.ContentRange{Start: 7, End: 9, Size: 10}, partial.ContentRange)
	require.EqualValues(t, 3, partial.ContentLength)
	requireDriverBody(t, partial.Body, "789")

	_, err = driver.OpenObject(t.Context(), storagedriver.OpenObjectRequest{Binding: binding, Key: key, Range: "bytes=10-"})
	require.Equal(t, driverError(storagedriver.CodeRangeNotSatisfiable), err)
	_, err = driver.OpenObject(t.Context(), storagedriver.OpenObjectRequest{Binding: binding, Key: key, Range: "bytes=1-2,3-4"})
	require.Equal(t, driverError(storagedriver.CodeInvalidRequest), err)

	err = driver.DeleteKeys(t.Context(), storagedriver.DeleteKeysRequest{Binding: binding, Keys: []string{key, "../outside"}})
	require.Equal(t, driverError(storagedriver.CodeInvalidRequest), err)
	require.Zero(t, deletes.Load())
	require.NoError(t, driver.DeleteKeys(t.Context(), storagedriver.DeleteKeysRequest{Binding: binding, Keys: []string{key, key}}))
	require.EqualValues(t, 1, deletes.Load())
}

func requireDriverBody(t *testing.T, body io.ReadCloser, want string) {
	t.Helper()
	defer func() { _ = body.Close() }()
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}

func TestS3DriverProbeProvesReadAndWriteAndCleansUp(t *testing.T) {
	var lock sync.Mutex
	var key, payload string
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		methods = append(methods, r.Method)
		w.Header().Set("ETag", `"probe"`)
		switch r.Method {
		case http.MethodPut:
			assert.Equal(t, "*", r.Header.Get("If-None-Match"))
			assert.True(t, strings.HasPrefix(r.URL.Path, "/bucket/scope/"))
			key = r.URL.Path
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			payload = string(body)
			w.Header().Set("X-Amz-Version-Id", "probe-version")
		case http.MethodGet:
			assert.Equal(t, key, r.URL.Path)
			assert.Equal(t, `"probe"`, r.Header.Get("If-Match"))
			_, _ = fmt.Fprint(w, payload)
		case http.MethodDelete:
			assert.Equal(t, key, r.URL.Path)
			assert.Equal(t, "probe-version", r.URL.Query().Get("versionId"))
			assert.Equal(t, `"probe"`, r.Header.Get("If-Match"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	require.NoError(t, NewS3Driver().TestConnection(t.Context(), testDriverBinding(server.URL)))
	lock.Lock()
	defer lock.Unlock()
	require.Equal(t, []string{http.MethodPut, http.MethodGet, http.MethodDelete}, methods)
}

func TestS3DriverListCursorScopesProviderPagination(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bucket", strings.TrimSuffix(r.URL.Path, "/"))
		query := r.URL.Query()
		assert.Equal(t, "scope/A", query.Get("prefix"))
		assert.Equal(t, "url", query.Get("encoding-type"))
		assert.Equal(t, "1", query.Get("max-keys"))
		w.Header().Set("Content-Type", "application/xml")
		switch calls.Add(1) {
		case 1:
			assert.Empty(t, query.Get("continuation-token"))
			_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated>
				<NextContinuationToken>provider+/=token</NextContinuationToken>
				<EncodingType>url</EncodingType><Contents><Key>scope/A%2B%252F</Key></Contents></ListBucketResult>`)
		case 2:
			assert.Equal(t, "provider+/=token", query.Get("continuation-token"))
			_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated>
				<Contents><Key>scope/A2</Key></Contents></ListBucketResult>`)
		default:
			_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated>
				<Contents><Key>scope-sibling/A3</Key></Contents></ListBucketResult>`)
		}
	}))
	defer server.Close()

	driver := NewS3Driver()
	req := storagedriver.ListPrefixRequest{Binding: testDriverBinding(server.URL), Prefix: "A", Limit: 1}
	first, err := driver.ListPrefix(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []string{"A+%2F"}, first.Keys)
	require.NotEmpty(t, first.NextCursor)
	req.Cursor = first.NextCursor

	other := req
	other.Binding.ID = "binding-2"
	_, err = driver.ListPrefix(t.Context(), other)
	require.Equal(t, driverError(storagedriver.CodeInvalidCursor), err)
	other = req
	other.Prefix = "B"
	_, err = driver.ListPrefix(t.Context(), other)
	require.Equal(t, driverError(storagedriver.CodeInvalidCursor), err)
	_, err = NewS3Driver().ListPrefix(t.Context(), req)
	require.Equal(t, driverError(storagedriver.CodeInvalidCursor), err)
	require.EqualValues(t, 1, calls.Load())

	second, err := driver.ListPrefix(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, storagedriver.ListPrefixResult{Keys: []string{"A2"}}, second)

	_, err = driver.ListPrefix(t.Context(), req)
	require.Equal(t, driverError(storagedriver.CodeInternal), err)
}

func TestS3DriverReportsEarlyEOFAndRedactsProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/truncated") {
			w.Header().Set("ETag", `"valid"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "100")
			_, _ = fmt.Fprint(w, "partial")
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>private provider detail</Message></Error>`)
	}))
	defer server.Close()

	driver := NewS3Driver()
	req := storagedriver.OpenObjectRequest{Binding: testDriverBinding(server.URL), Key: "truncated"}
	result, err := driver.OpenObject(t.Context(), req)
	require.NoError(t, err)
	defer func() { _ = result.Body.Close() }()
	_, err = io.ReadAll(result.Body)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)

	req.Key = "denied"
	_, err = driver.OpenObject(t.Context(), req)
	require.Equal(t, driverError(storagedriver.CodeAccessDenied), err)
}

func TestS3DriverAbortsUnfinishedMultipartUpload(t *testing.T) {
	var aborted atomic.Bool
	var completed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bucket/scope/large", r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			if !r.URL.Query().Has("uploads") {
				completed.Store(true)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>temporary</UploadId></InitiateMultipartUploadResult>`)
		case http.MethodPut:
			assert.Equal(t, "temporary", r.URL.Query().Get("uploadId"))
			n, err := io.Copy(io.Discard, r.Body)
			assert.NoError(t, err)
			assert.EqualValues(t, driverPartSize, n)
			w.Header().Set("ETag", `"first-part"`)
		case http.MethodDelete:
			assert.Equal(t, "temporary", r.URL.Query().Get("uploadId"))
			aborted.Store(true)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	body := &failingUploadReader{remaining: driverPartSize}
	_, err := NewS3Driver().PutObject(t.Context(), storagedriver.PutObjectRequest{
		Binding: testDriverBinding(server.URL), Key: "large", ContentType: storagedriver.MediaTypeBinary,
	}, body)
	require.Equal(t, driverError(storagedriver.CodeInternal), err)
	require.True(t, aborted.Load())
	require.False(t, completed.Load())
}

type failingUploadReader struct{ remaining int }

func (r *failingUploadReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, errors.New("private source error")
	}
	n := min(len(p), r.remaining)
	r.remaining -= n
	clear(p[:n])
	return n, nil
}

func TestS3DriverRejectsRedirects(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := NewS3Driver().OpenObject(t.Context(), storagedriver.OpenObjectRequest{
		Binding: testDriverBinding(server.URL), Key: "object",
	})
	require.Equal(t, driverError(storagedriver.CodeUnavailable), err)
	require.False(t, redirected.Load())
}
