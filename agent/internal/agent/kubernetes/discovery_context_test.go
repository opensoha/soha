package kubernetes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/flowcontrol"
)

func TestResourceCreateDiscoveryHTTPCancellationStopsBeforeCreate(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			entered, ended := make(chan struct{}), make(chan struct{}, 32)
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { close(entered) })
				<-r.Context().Done()
				select {
				case ended <- struct{}{}:
				default:
				}
			}))
			defer server.Close()
			client := discoveryHTTPTestClient(t, server.URL)
			var creates atomic.Int32
			client.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "*", func(ktesting.Action) (bool, runtime.Object, error) {
				creates.Add(1)
				return true, nil, errors.New("must not create")
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				defer cancel()
			}
			done := make(chan domainresource.KubernetesResourceAgentCreateResult, 1)
			go func() {
				done <- client.CreateResources(ctx, resourceCreateRequest(configMapCreateDocument(0, "platform", "app")))
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("discovery HTTP never started")
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case result := <-done:
				if result.Status != domainresource.KubernetesResourceCreateBatchStatusFailed || result.Items[0].Error == nil {
					t.Fatalf("result=%+v", result)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("discovery did not observe cancellation")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("discovery HTTP remained blocked")
			}
			if creates.Load() != 0 {
				t.Fatalf("create calls=%d", creates.Load())
			}
		})
	}
}

func TestResourceDiscoveryRejectsAlreadyCancelledBeforeHTTPOrFake(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	for _, realHTTP := range []bool{false, true} {
		client, _ := newResourceCreationTestClient(t)
		if realHTTP {
			client = discoveryHTTPTestClient(t, server.URL)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.resourceCreateMapper(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("realHTTP=%v err=%v", realHTTP, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("cancelled discovery issued %d requests", requests.Load())
	}
}

func TestResourceDiscoveryRetainsFakeInjection(t *testing.T) {
	client, _ := newResourceCreationTestClient(t)
	discoveryClient, err := client.discoveryForContext(context.Background())
	if err != nil || any(discoveryClient) != any(client.discovery) {
		t.Fatalf("fake replaced: %T err=%v", discoveryClient, err)
	}
	mapper, err := client.resourceCreateMapper(context.Background())
	if err != nil || mapper == nil {
		t.Fatalf("fake mapper=%v err=%v", mapper, err)
	}
}

func TestResourceDiscoveryPreservesTransportAuthenticationAndBodyLifetime(t *testing.T) {
	var wrappers atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer discovery-token" || r.Header.Get("X-Existing-Wrapper") != "retained" {
			t.Errorf("auth/wrapper lost: %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// RoundTrip has returned headers; its operation context must still cover body reads.
		select {
		case <-r.Context().Done():
			t.Error("response body cancelled before consumption")
			return
		case <-time.After(20 * time.Millisecond):
		}
		_, _ = io.WriteString(w, `{"gitVersion":"v1.37.1"}`)
	}))
	defer server.Close()
	client := discoveryHTTPTestClient(t, server.URL)
	client.restConfig.BearerToken = "discovery-token"
	client.restConfig.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return discoveryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			wrappers.Add(1)
			clone := r.Clone(r.Context())
			clone.Header.Set("X-Existing-Wrapper", "retained")
			return next.RoundTrip(clone)
		})
	})
	original := client.restConfig.WrapTransport
	var buildErr error
	client.discovery, buildErr = discovery.NewDiscoveryClientForConfig(client.restConfig)
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dc, err := client.discoveryForContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	version, err := dc.ServerVersionWithContext(ctx)
	if err != nil || version.GitVersion != "v1.37.1" || wrappers.Load() != 1 {
		t.Fatalf("version=%+v err=%v wrappers=%d", version, err, wrappers.Load())
	}
	// Building an operation client must leave the shared config's wrapper intact.
	probe := original(discoveryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Existing-Wrapper") != "retained" {
			t.Error("shared wrapper mutated")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	response, err := probe.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func TestResourceDiscoveryTotalBudgetReachesThrottleWait(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	client := discoveryHTTPTestClient(t, server.URL)
	limiter := flowcontrol.NewTokenBucketRateLimiter(1, 1)
	if !limiter.TryAccept() {
		t.Fatal("initial token unavailable")
	}
	client.restConfig.RateLimiter = limiter
	var err error
	client.discovery, err = discovery.NewDiscoveryClientForConfig(client.restConfig)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.resourceCreateMapper(ctx); err == nil {
		t.Fatal("throttle ignored operation deadline")
	}
	if time.Since(started) > time.Second || requests.Load() != 0 {
		t.Fatalf("throttle exceeded budget or reached HTTP: elapsed=%v requests=%d", time.Since(started), requests.Load())
	}
}

func discoveryHTTPTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	config := &rest.Config{Host: endpoint, Timeout: 5 * time.Second, QPS: -1}
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{cfg: cfgpkg.KubernetesConfig{ID: "agent-cluster"}, restConfig: config, discovery: dc, dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())}
}

type discoveryTestRoundTripper func(*http.Request) (*http.Response, error)

func (f discoveryTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCRDSummaryDiscoveryHTTPObservesQueryCancellation(t *testing.T) {
	entered, ended := make(chan struct{}), make(chan struct{}, 32)
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		select {
		case ended <- struct{}{}:
		default:
		}
	}))
	defer server.Close()
	client := discoveryHTTPTestClient(t, server.URL)
	scheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	client.metadata = metadatafake.NewSimpleMetadataClient(scheme, &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition"}, ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.io"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.listCRDSummaries(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("CRD discovery did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CRD discovery ignored query cancellation")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("CRD discovery HTTP remained blocked")
	}
}

func TestResourceCreateMapperBindsEightSecondTotalDiscoveryDeadline(t *testing.T) {
	var observed atomic.Bool
	client := discoveryHTTPTestClient(t, "http://discovery.invalid")
	client.restConfig.Timeout = 0
	client.restConfig.Transport = discoveryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > resourceDiscoveryTimeout || time.Until(deadline) < resourceDiscoveryTimeout-time.Second {
			t.Errorf("discovery total deadline=%v present=%v", deadline, ok)
		}
		observed.Store(true)
		return nil, errors.New("stop discovery after deadline observation")
	})
	var buildErr error
	client.discovery, buildErr = discovery.NewDiscoveryClientForConfig(client.restConfig)
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	if _, err := client.resourceCreateMapper(context.Background()); err == nil {
		t.Fatal("failed discovery unexpectedly mapped resources")
	}
	if !observed.Load() {
		t.Fatal("discovery transport was not reached")
	}
}

func TestResourceDiscoveryCancellationInterruptsRetryAfter(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":429}`)
		once.Do(func() { close(entered) })
	}))
	defer server.Close()
	client := discoveryHTTPTestClient(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.resourceCreateMapper(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("retry response not received")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("REST Retry-After wait ignored cancellation")
	}
}
