package sdktest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk/sdktest"
)

type testDriver struct {
	firstUploadRead chan struct{}
	streamClosed    chan struct{}
	mu              sync.Mutex
	object          []byte
}

func (d *testDriver) TestConnection(_ context.Context, binding storagedriver.Binding) error {
	if binding.Credentials["credential"] != "synthetic" {
		return storagedriver.RPCError{Code: storagedriver.CodeAccessDenied}
	}
	return nil
}

func (d *testDriver) PutObject(_ context.Context, _ storagedriver.PutObjectRequest, body io.Reader) (storagedriver.PutObjectResult, error) {
	var object bytes.Buffer
	if _, err := io.CopyN(&object, body, 1); err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	close(d.firstUploadRead)
	if _, err := io.Copy(&object, body); err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	data := object.Bytes()
	d.mu.Lock()
	d.object = data
	d.mu.Unlock()
	digest := sha256.Sum256(data)
	return storagedriver.PutObjectResult{Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), ETag: `"fixture"`}, nil
}

func (d *testDriver) OpenObject(_ context.Context, req storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	result := storagedriver.OpenObjectResult{ETag: `"fixture"`}
	if storagedriver.MatchesIfNoneMatch(req.IfNoneMatch, result.ETag) {
		result.NotModified = true
		return result, nil
	}
	result.ContentType = storagedriver.MediaTypeBinary
	d.mu.Lock()
	data := d.object
	d.mu.Unlock()
	result.ContentLength = int64(len(data))
	switch req.Key {
	case "waiting":
		result.ContentLength = 1
		result.Body = &waitingReader{closed: d.streamClosed}
		return result, nil
	case "short":
		result.ContentLength = 2
		result.Body = io.NopCloser(strings.NewReader("x"))
		return result, nil
	}
	span, err := storagedriver.ResolveByteRange(req.Range, result.ContentLength)
	if err != nil {
		return storagedriver.OpenObjectResult{}, err
	}
	if span != nil {
		result.ContentRange = span
		data = data[span.Start : span.End+1]
		result.ContentLength = int64(len(data))
	}
	result.Body = io.NopCloser(bytes.NewReader(data))
	return result, nil
}

func (d *testDriver) DeleteKeys(_ context.Context, req storagedriver.DeleteKeysRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, key := range req.Keys {
		if key == "objects/fixture" {
			d.object = nil
		}
	}
	return nil
}

func (d *testDriver) ListPrefix(_ context.Context, req storagedriver.ListPrefixRequest) (storagedriver.ListPrefixResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if req.Cursor == "" && len(d.object) != 0 {
		return storagedriver.ListPrefixResult{Keys: []string{"objects/fixture"}, NextCursor: "end"}, nil
	}
	return storagedriver.ListPrefixResult{Keys: []string{}}, nil
}

type waitingReader struct{ closed chan struct{} }

func (r *waitingReader) Read(_ []byte) (int, error) {
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *waitingReader) Close() error {
	close(r.closed)
	return nil
}

func TestAuthorClientStorageOperations(t *testing.T) {
	driver := &testDriver{firstUploadRead: make(chan struct{})}
	client := sdktest.NewClient(t, sdk.ServerOptions{PluginID: "example.storage", Version: "1.2.3"}, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	binding := storagedriver.Binding{ID: "fixture", Container: "objects", Prefix: "tenant/",
		Credentials: map[string]string{"credential": "synthetic"}}
	if err := client.TestConnection(ctx, binding); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	defer reader.Close()
	input := bytes.Repeat([]byte("ABC"), 1<<20)
	putReq := storagedriver.PutObjectRequest{Binding: binding, Key: "objects/fixture", ContentType: storagedriver.MediaTypeBinary}
	transfer := make(chan error, 1)
	go func() {
		if _, err := writer.Write(input[:32768]); err != nil {
			transfer <- err
			return
		}
		select {
		case <-driver.firstUploadRead:
			_, err := writer.Write(input[32768:])
			_ = writer.CloseWithError(err)
			transfer <- err
		case <-ctx.Done():
			_ = writer.CloseWithError(ctx.Err())
			transfer <- ctx.Err()
		}
	}()
	put, err := client.PutObject(ctx, putReq, reader)
	if err != nil || put.Size != int64(len(input)) {
		t.Fatalf("stream put: size %d, error %v", put.Size, err)
	}
	if err := <-transfer; err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(input)
	if put.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("incorrect stream digest")
	}
	openReq := storagedriver.OpenObjectRequest{Binding: binding, Key: putReq.Key, Range: "bytes=1-3"}
	opened, err := client.OpenObject(ctx, openReq)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(opened.Body)
	_ = opened.Body.Close()
	if readErr != nil || opened.ContentRange == nil || opened.ContentRange.Start != 1 || string(got) != "BCA" {
		t.Fatalf("read range: %q, %v", got, readErr)
	}
	openReq.IfNoneMatch = `W/"fixture"`
	conditional, err := client.OpenObject(ctx, openReq)
	if err != nil || !conditional.NotModified || conditional.Body != nil {
		t.Fatalf("open condition: %+v, %v", conditional, err)
	}
	openReq.IfNoneMatch, openReq.Range = "", ""
	full, err := client.OpenObject(ctx, openReq)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr = io.ReadAll(full.Body)
	_ = full.Body.Close()
	if readErr != nil || !bytes.Equal(got, input) {
		t.Fatalf("full stream: got %d bytes, error %v", len(got), readErr)
	}
	listReq := storagedriver.ListPrefixRequest{Binding: binding, Prefix: "objects/", Limit: 1}
	page, err := client.ListPrefix(ctx, listReq)
	if err != nil || len(page.Keys) != 1 || page.NextCursor == "" {
		t.Fatalf("first page: %+v, %v", page, err)
	}
	listReq.Cursor = page.NextCursor
	page, err = client.ListPrefix(ctx, listReq)
	if err != nil || len(page.Keys) != 0 || page.NextCursor != "" {
		t.Fatalf("final page: %+v, %v", page, err)
	}
	deleteReq := storagedriver.DeleteKeysRequest{Binding: binding, Keys: []string{putReq.Key, putReq.Key}}
	for range 2 {
		if err := client.DeleteKeys(ctx, deleteReq); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInterruptedOpenReleasesOnlyThatStream(t *testing.T) {
	for _, method := range []string{"close", "cancel", "deadline"} {
		t.Run(method, func(t *testing.T) {
			driver := &testDriver{streamClosed: make(chan struct{})}
			client := sdktest.NewClient(t, sdk.ServerOptions{PluginID: "example.storage", Version: "1.2.3"}, driver)
			binding := storagedriver.Binding{ID: "fixture", Container: "objects", Credentials: map[string]string{"credential": "synthetic"}}
			window := 5 * time.Second
			if method == "deadline" {
				window = 200 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), window)
			defer cancel()
			opened, err := client.OpenObject(ctx, storagedriver.OpenObjectRequest{Binding: binding, Key: "waiting"})
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Body.Close()
			if err := client.TestConnection(context.Background(), binding); err != nil {
				t.Fatalf("another request waited for the open stream: %v", err)
			}
			switch method {
			case "close":
				if err := opened.Body.Close(); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
				if _, err := io.ReadAll(opened.Body); !errors.Is(err, context.Canceled) {
					t.Fatalf("body did not observe cancellation: %v", err)
				}
			case "deadline":
				if _, err := io.ReadAll(opened.Body); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("body did not observe deadline: %v", err)
				}
			}
			select {
			case <-driver.streamClosed:
			case <-time.After(5 * time.Second):
				t.Fatal("provider stream did not close")
			}
			if err := client.TestConnection(context.Background(), binding); err != nil {
				t.Fatalf("cancelling one call broke the client: %v", err)
			}
		})
	}
}

func TestShortOpenStreamFails(t *testing.T) {
	client := sdktest.NewClient(t, sdk.ServerOptions{PluginID: "example.storage", Version: "1.2.3"}, &testDriver{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opened, err := client.OpenObject(ctx, storagedriver.OpenObjectRequest{
		Binding: storagedriver.Binding{ID: "fixture", Container: "objects"}, Key: "short",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Body.Close()
	if _, err := io.ReadAll(opened.Body); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short stream succeeded or returned the wrong error: %v", err)
	}
}

type uploadCancelDriver struct {
	testDriver
	started  chan struct{}
	finished chan error
}

func (d *uploadCancelDriver) PutObject(ctx context.Context, _ storagedriver.PutObjectRequest, body io.Reader) (storagedriver.PutObjectResult, error) {
	if _, err := io.CopyN(io.Discard, body, 1); err != nil {
		return storagedriver.PutObjectResult{}, err
	}
	close(d.started)
	_, err := io.Copy(io.Discard, body)
	d.finished <- ctx.Err()
	return storagedriver.PutObjectResult{}, err
}

func TestCancelledUploadStopsProviderRead(t *testing.T) {
	driver := &uploadCancelDriver{started: make(chan struct{}), finished: make(chan error, 1)}
	client := sdktest.NewClient(t, sdk.ServerOptions{PluginID: "example.storage", Version: "1.2.3"}, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipe, writer := io.Pipe()
	defer pipe.Close()
	defer writer.Close()
	request := storagedriver.PutObjectRequest{
		Binding: storagedriver.Binding{ID: "fixture", Container: "objects"}, Key: "stream", ContentType: storagedriver.MediaTypeBinary,
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.PutObject(ctx, request, pipe)
		done <- err
	}()
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-driver.started:
	case <-ctx.Done():
		t.Fatal("upload did not start before its producer closed")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("upload did not observe cancellation: %v", err)
	}
	select {
	case err := <-driver.finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("provider did not observe cancelled upload: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider reader did not stop")
	}
}
