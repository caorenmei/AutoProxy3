package rulesources

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caorenmei/autoproxy3/src/internal/rules"
)

func TestWebSourceLoadDownloadsAndCachesRules(t *testing.T) {
	body := encodedWebRules(
		"[AutoProxy 0.2.9]",
		"||example.com",
		"@@||direct.example.com",
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	source := WebSource{URL: server.URL, CachePath: cachePath}

	set, fromRemote, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !fromRemote {
		t.Fatal("expected remote load")
	}
	if !set.ProxyHost("example.com") {
		t.Fatal("expected proxy rule from remote source")
	}
	if !set.DirectHost("direct.example.com") {
		t.Fatal("expected direct rule from remote source")
	}

	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(cached) != body {
		t.Fatalf("unexpected cached body: got %q want %q", string(cached), body)
	}
}

func TestWebSourceLoadMarksUpstreamRequests(t *testing.T) {
	body := encodedWebRules("[AutoProxy 0.2.9]", "||example.com")
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get(useUpstreamHeader)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	source := WebSource{
		URL:       server.URL,
		CachePath: filepath.Join(t.TempDir(), "web_rules.txt"),
		ShouldUseProxy: func(rawURL string) bool {
			return rawURL == server.URL
		},
	}

	_, _, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	select {
	case value := <-headers:
		if value != "true" {
			t.Fatalf("unexpected upstream header: %q", value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
	}
}

func TestWebSourceLoadFallsBackToCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	cachedBody := encodedWebRules("[AutoProxy 0.2.9]", "||cached.example.com")
	if err := os.WriteFile(cachePath, []byte(cachedBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := WebSource{
		URL:       "http://127.0.0.1:1",
		CachePath: cachePath,
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed")
			}),
		},
	}

	set, fromRemote, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if fromRemote {
		t.Fatal("expected cache fallback")
	}
	if !set.ProxyHost("cached.example.com") {
		t.Fatal("expected cached rules to load")
	}
}

func TestWebSourceLoadFallsBackToCacheWhenRequestBuildFails(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	cachedBody := encodedWebRules("[AutoProxy 0.2.9]", "||cached.example.com")
	if err := os.WriteFile(cachePath, []byte(cachedBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := WebSource{URL: "://bad-url", CachePath: cachePath}

	set, fromRemote, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if fromRemote {
		t.Fatal("expected cache fallback")
	}
	if !set.ProxyHost("cached.example.com") {
		t.Fatal("expected cached rules to load")
	}
}

func TestWebSourceLoadFallsBackToCacheWhenResponseReadFails(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	cachedBody := encodedWebRules("[AutoProxy 0.2.9]", "||cached.example.com")
	if err := os.WriteFile(cachePath, []byte(cachedBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := WebSource{
		URL:       "http://rules.example.com",
		CachePath: cachePath,
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       errReadCloser{},
				}, nil
			}),
		},
	}

	set, fromRemote, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if fromRemote {
		t.Fatal("expected cache fallback")
	}
	if !set.ProxyHost("cached.example.com") {
		t.Fatal("expected cached rules to load")
	}
}

func TestWebSourceLoadFallsBackToCacheWhenRemoteParseFails(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	cachedBody := encodedWebRules("[AutoProxy 0.2.9]", "||cached.example.com")
	if err := os.WriteFile(cachePath, []byte(cachedBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-base64"))
	}))
	defer server.Close()

	source := WebSource{URL: server.URL, CachePath: cachePath}

	set, fromRemote, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if fromRemote {
		t.Fatal("expected cache fallback")
	}
	if !set.ProxyHost("cached.example.com") {
		t.Fatal("expected cached rules to load")
	}
}

func TestWebSourceLoadFallsBackToCacheWhenStatusIsNotSuccess(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	cachedBody := encodedWebRules("[AutoProxy 0.2.9]", "||cached.example.com")
	if err := os.WriteFile(cachePath, []byte(cachedBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	body := encodedWebRules("[AutoProxy 0.2.9]", "||remote.example.com")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	source := WebSource{URL: server.URL, CachePath: cachePath}

	set, fromRemote, err := source.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if fromRemote {
		t.Fatal("expected cache fallback")
	}
	if !set.ProxyHost("cached.example.com") {
		t.Fatal("expected cached rules to load")
	}
	if set.ProxyHost("remote.example.com") {
		t.Fatal("expected failed remote response to be ignored")
	}
}

func TestWebSourceLoadReturnsErrorWhenRemoteAndCacheFail(t *testing.T) {
	source := WebSource{
		URL:       "://bad-url",
		CachePath: filepath.Join(t.TempDir(), "missing.txt"),
	}

	_, _, err := source.Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWebSourceLoadReturnsErrorWhenCacheWriteFails(t *testing.T) {
	body := encodedWebRules("[AutoProxy 0.2.9]", "||example.com")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	source := WebSource{URL: server.URL, CachePath: t.TempDir()}

	_, _, err := source.Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWebSourceStartRefreshLoopDoesNotStartWhenIntervalIsNonPositive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan struct{}, 1)
	source := WebSource{URL: "http://127.0.0.1:1", CachePath: filepath.Join(t.TempDir(), "web_rules.txt")}
	source.StartRefreshLoop(ctx, 0, func(rules.WebRuleSet) {
		called <- struct{}{}
	})

	select {
	case <-called:
		t.Fatal("expected refresh loop to stay stopped")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWebSourceStartRefreshLoopSkipsFailedLoads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	applied := make(chan struct{}, 1)
	source := WebSource{URL: "://bad-url", CachePath: filepath.Join(t.TempDir(), "missing.txt")}
	source.StartRefreshLoop(ctx, 10*time.Millisecond, func(rules.WebRuleSet) {
		applied <- struct{}{}
	})

	select {
	case <-applied:
		t.Fatal("expected failed refresh to skip apply")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWebSourceStartRefreshLoopSkipsCacheFallbackResults(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "web_rules.txt")
	cachedBody := encodedWebRules("[AutoProxy 0.2.9]", "||cached.example.com")
	if err := os.WriteFile(cachePath, []byte(cachedBody), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	applied := make(chan struct{}, 1)
	source := WebSource{
		URL:       "://bad-url",
		CachePath: cachePath,
	}
	source.StartRefreshLoop(ctx, 10*time.Millisecond, func(rules.WebRuleSet) {
		applied <- struct{}{}
	})

	select {
	case <-applied:
		t.Fatal("expected cache fallback refresh to skip apply")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWebSourceStartRefreshLoopAppliesSuccessfulLoads(t *testing.T) {
	body := encodedWebRules("[AutoProxy 0.2.9]", "||example.com")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	applied := make(chan rules.WebRuleSet, 1)
	source := WebSource{URL: server.URL, CachePath: filepath.Join(t.TempDir(), "web_rules.txt")}
	source.StartRefreshLoop(ctx, 10*time.Millisecond, func(set rules.WebRuleSet) {
		applied <- set
		cancel()
	})

	select {
	case set := <-applied:
		if !set.ProxyHost("example.com") {
			t.Fatal("expected applied set to contain remote rule")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for apply")
	}
}

func TestWebSourceStartRefreshLoopStopsAfterContextCancel(t *testing.T) {
	body := encodedWebRules("[AutoProxy 0.2.9]", "||example.com")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var appliedCount atomic.Int32
	stopped := make(chan struct{})
	source := WebSource{URL: server.URL, CachePath: filepath.Join(t.TempDir(), "web_rules.txt")}
	source.StartRefreshLoop(ctx, 10*time.Millisecond, func(rules.WebRuleSet) {
		if appliedCount.Add(1) == 1 {
			cancel()
			close(stopped)
		}
	})

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first apply")
	}

	time.Sleep(50 * time.Millisecond)
	if got := appliedCount.Load(); got != 1 {
		t.Fatalf("expected apply count to stop at 1, got %d", got)
	}
}

func TestWebSourceStartRefreshLoopCancelsInFlightRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCancelled := make(chan struct{})

	source := WebSource{
		URL:       "http://rules.example.com",
		CachePath: filepath.Join(t.TempDir(), "missing.txt"),
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				close(requestStarted)
				<-r.Context().Done()
				close(requestCancelled)
				return nil, r.Context().Err()
			}),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	source.StartRefreshLoop(ctx, 10*time.Millisecond, func(rules.WebRuleSet) {
		t.Fatal("expected failed refresh to skip apply")
	})

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for refresh request")
	}

	cancel()

	select {
	case <-requestCancelled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected in-flight request to be cancelled")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReadCloser) Close() error {
	return nil
}

func encodedWebRules(lines ...string) string {
	return base64.StdEncoding.EncodeToString([]byte(joinLines(lines...)))
}

func joinLines(lines ...string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for _, line := range lines[1:] {
		result += "\n" + line
	}
	return result
}
