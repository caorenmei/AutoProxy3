package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/caorenmei/autoproxy3/src/internal/rules"
)

func TestServerHTTPDirectSuccess(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, "direct-ok")
	}))
	defer target.Close()

	server := NewServer(Options{
		Engine: rules.NewEngine(),
		Logger: newTestLogger(io.Discard),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, target.URL+"/hello")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body != "direct-ok" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestServerCONNECTDirectSuccess(t *testing.T) {
	targetAddr, closeTarget := startEchoServer(t)
	defer closeTarget()

	server := NewServer(Options{
		Engine: rules.NewEngine(),
		Logger: newTestLogger(io.Discard),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	conn := mustOpenTunnel(t, proxyServer.URL, targetAddr)
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatalf("read tunnel payload: %v", err)
	}
	if string(buffer) != "ping" {
		t.Fatalf("unexpected tunnel echo: %q", string(buffer))
	}
}

func TestServerHTTPViaUpstreamConnectSuccess(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "upstream-http-ok")
	}))
	defer target.Close()

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	upstream := newFakeUpstream(t, map[string]string{targetURL.Host: targetURL.Host})
	defer upstream.Close()

	engine := rules.NewEngine()
	engine.ReplaceCustomRules(newHostRuleSet(t, targetURL.Hostname()))

	server := NewServer(Options{
		Engine:        engine,
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: upstream.Address(),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, target.URL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body != "upstream-http-ok" {
		t.Fatalf("unexpected body: %q", body)
	}
	if got := upstream.ConnectCount(targetURL.Host); got != 1 {
		t.Fatalf("expected 1 upstream CONNECT, got %d", got)
	}
}

func TestServerCONNECTViaUpstreamConnectSuccess(t *testing.T) {
	targetAddr, closeTarget := startEchoServer(t)
	defer closeTarget()

	upstream := newFakeUpstream(t, map[string]string{targetAddr: targetAddr})
	defer upstream.Close()

	host, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	engine := rules.NewEngine()
	engine.ReplaceCustomRules(newHostRuleSet(t, host))

	server := NewServer(Options{
		Engine:        engine,
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: upstream.Address(),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	conn := mustOpenTunnel(t, proxyServer.URL, targetAddr)
	defer conn.Close()

	if _, err := conn.Write([]byte("pong")); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatalf("read tunnel payload: %v", err)
	}
	if string(buffer) != "pong" {
		t.Fatalf("unexpected tunnel echo: %q", string(buffer))
	}
	if got := upstream.ConnectCount(targetAddr); got != 1 {
		t.Fatalf("expected 1 upstream CONNECT, got %d", got)
	}
}

func TestServerRuleRequiresUpstreamWithoutConfigFallsBackToDirectAndWarns(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fallback-direct-ok")
	}))
	defer target.Close()

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	engine := rules.NewEngine()
	engine.ReplaceCustomRules(newHostRuleSet(t, targetURL.Hostname()))

	var logs bytes.Buffer
	server := NewServer(Options{
		Engine: engine,
		Logger: newTestLogger(&logs),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, target.URL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body != "fallback-direct-ok" {
		t.Fatalf("unexpected body: %q", body)
	}
	if !strings.Contains(logs.String(), "upstream proxy is not configured") {
		t.Fatalf("expected warn log, got %q", logs.String())
	}
}

func TestServerAutoDetectFallsBackAfterDirectDialFailuresAndRecordsHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "auto-detect-ok")
	}))
	defer target.Close()

	recorder := &memoryRecorder{}
	upstream := newFakeUpstream(t, map[string]string{"autodetect.example:80": strings.TrimPrefix(target.URL, "http://")})
	defer upstream.Close()

	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         upstream.Address(),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 2,
		AutoDetectRecorder:    recorder,
		DialContext:           failingDialerForHost(t, "autodetect.example"),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp1, body1 := mustDoProxyRequest(t, proxyServer.URL, "http://autodetect.example/first")
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected first request to fail with 502, got %d", resp1.StatusCode)
	}
	if recorder.Count() != 0 {
		t.Fatalf("expected no record on first failure, got %d", recorder.Count())
	}
	if strings.Contains(body1, "auto-detect-ok") {
		t.Fatalf("unexpected first response body: %q", body1)
	}

	resp2, body2 := mustDoProxyRequest(t, proxyServer.URL, "http://autodetect.example/second")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected second request to succeed, got %d", resp2.StatusCode)
	}
	if body2 != "auto-detect-ok" {
		t.Fatalf("unexpected second response body: %q", body2)
	}
	if recorder.Count() != 1 || recorder.LastHost() != "autodetect.example" {
		t.Fatalf("unexpected recorder state: count=%d host=%q", recorder.Count(), recorder.LastHost())
	}
	decision := server.engine.Decide("autodetect.example")
	if decision.Source != rules.DecisionSourceAutoDetect {
		t.Fatalf("expected engine to refresh auto-detect rule, got %+v", decision)
	}
}

func TestServerAutoDetectDoesNotTriggerOnHTTPStatusErrors(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server-error", http.StatusInternalServerError)
	}))
	defer target.Close()

	recorder := &memoryRecorder{}
	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorder,
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, target.URL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "server-error") {
		t.Fatalf("unexpected body: %q", body)
	}
	if recorder.Count() != 0 {
		t.Fatalf("expected no auto-detect record, got %d", recorder.Count())
	}
}

func TestServerAutoDetectDoesNotTriggerOnNonTCPRoundTripErrors(t *testing.T) {
	recorder := &memoryRecorder{}
	var calls atomic.Int32
	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         "127.0.0.1:1",
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorder,
		NewRoundTripper: func(DialContext) http.RoundTripper {
			return roundTripperFunc(func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return nil, errors.New("application layer failure")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("unexpected-fallback")),
				}, nil
			})
		},
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, "http://roundtrip-error.example/test")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	if strings.Contains(body, "unexpected-fallback") {
		t.Fatalf("unexpected fallback response body: %q", body)
	}
	if recorder.Count() != 0 {
		t.Fatalf("expected no auto-detect record, got %d", recorder.Count())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 RoundTrip call, got %d", got)
	}
}

func TestServerAutoDetectTriggersOnDirectTCPDialFailureInHTTPPath(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "tcp-fallback-ok")
	}))
	defer target.Close()

	recorder := &memoryRecorder{}
	upstream := newFakeUpstream(t, map[string]string{"tcp-failure.example:80": strings.TrimPrefix(target.URL, "http://")})
	defer upstream.Close()

	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         upstream.Address(),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorder,
		DialContext:           failingDialerForHost(t, "tcp-failure.example"),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, "http://tcp-failure.example/test")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body != "tcp-fallback-ok" {
		t.Fatalf("unexpected body: %q", body)
	}
	if recorder.Count() != 1 || recorder.LastHost() != "tcp-failure.example" {
		t.Fatalf("unexpected recorder state: count=%d host=%q", recorder.Count(), recorder.LastHost())
	}
	if got := upstream.ConnectCount("tcp-failure.example:80"); got != 1 {
		t.Fatalf("expected 1 upstream CONNECT, got %d", got)
	}
}

func TestServerAutoDetectStoreFailureLogsErrorAndDoesNotRefreshRule(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "store-failure-ok")
	}))
	defer target.Close()

	upstream := newFakeUpstream(t, map[string]string{"record-fail.example:80": strings.TrimPrefix(target.URL, "http://")})
	defer upstream.Close()

	var logs bytes.Buffer
	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(&logs),
		UpstreamProxy:         upstream.Address(),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorderFunc(func(context.Context, string) error { return errors.New("write failed") }),
		DialContext:           failingDialerForHost(t, "record-fail.example"),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, body := mustDoProxyRequest(t, proxyServer.URL, "http://record-fail.example/test")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body != "store-failure-ok" {
		t.Fatalf("unexpected body: %q", body)
	}
	if !strings.Contains(logs.String(), "record auto-detect host") {
		t.Fatalf("expected error log, got %q", logs.String())
	}
	decision := server.engine.Decide("record-fail.example")
	if decision.Source != rules.DecisionSourceDefault {
		t.Fatalf("expected rule refresh to stay inactive, got %+v", decision)
	}
}

func TestServerEndToEndWithFakeUpstream(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Target", "reachable")
		fmt.Fprint(w, "integration-ok")
	}))
	defer target.Close()

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	upstream := newFakeUpstream(t, map[string]string{targetURL.Host: targetURL.Host})
	defer upstream.Close()

	engine := rules.NewEngine()
	engine.ReplaceCustomRules(newHostRuleSet(t, targetURL.Hostname()))

	server := NewServer(Options{
		Engine:        engine,
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: upstream.Address(),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	client := &http.Client{
		Transport: &http.Transport{Proxy: mustProxyURL(t, proxyServer.URL)},
		Timeout:   5 * time.Second,
	}

	resp, err := client.Get(target.URL + "/integration")
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.Header.Get("X-Target") != "reachable" {
		t.Fatalf("unexpected response header: %q", resp.Header.Get("X-Target"))
	}
	if string(bodyBytes) != "integration-ok" {
		t.Fatalf("unexpected response body: %q", string(bodyBytes))
	}
	if got := upstream.ConnectCount(targetURL.Host); got != 1 {
		t.Fatalf("expected upstream CONNECT count 1, got %d", got)
	}
}

type memoryRecorder struct {
	mu    sync.Mutex
	hosts []string
}

func (r *memoryRecorder) Record(_ context.Context, host string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hosts = append(r.hosts, host)
	return nil
}

func (r *memoryRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hosts)
}

func (r *memoryRecorder) LastHost() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.hosts) == 0 {
		return ""
	}
	return r.hosts[len(r.hosts)-1]
}

type recorderFunc func(context.Context, string) error

func (f recorderFunc) Record(ctx context.Context, host string) error {
	return f(ctx, host)
}

type fakeUpstream struct {
	t        *testing.T
	listener net.Listener
	routes   map[string]string
	counts   sync.Map
	closed   atomic.Bool
}

func newFakeUpstream(t *testing.T, routes map[string]string) *fakeUpstream {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake upstream: %v", err)
	}

	u := &fakeUpstream{
		t:        t,
		listener: listener,
		routes:   routes,
	}
	go u.serve()
	return u
}

func (u *fakeUpstream) Address() string {
	return u.listener.Addr().String()
}

func (u *fakeUpstream) Close() {
	if u.closed.CompareAndSwap(false, true) {
		_ = u.listener.Close()
	}
}

func (u *fakeUpstream) ConnectCount(target string) int {
	value, ok := u.counts.Load(target)
	if !ok {
		return 0
	}
	return int(value.(int32))
}

func (u *fakeUpstream) serve() {
	for {
		conn, err := u.listener.Accept()
		if err != nil {
			if u.closed.Load() {
				return
			}
			return
		}
		go u.handleConn(conn)
	}
}

func (u *fakeUpstream) handleConn(clientConn net.Conn) {
	defer clientConn.Close()

	reader := bufio.NewReader(clientConn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		u.t.Errorf("read upstream request: %v", err)
		return
	}
	if req.Method != http.MethodConnect {
		u.t.Errorf("unexpected upstream method: %s", req.Method)
		return
	}

	targetAddr := req.Host
	actualAddr := u.routes[targetAddr]
	if actualAddr == "" {
		http.Error(newResponseWriter(clientConn), "missing route", http.StatusBadGateway)
		return
	}

	counter, _ := u.counts.LoadOrStore(targetAddr, int32(0))
	u.counts.Store(targetAddr, counter.(int32)+1)

	targetConn, err := net.Dial("tcp", actualAddr)
	if err != nil {
		fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer targetConn.Close()

	fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")

	errCh := make(chan error, 2)
	go proxyCopy(errCh, targetConn, reader)
	go proxyCopy(errCh, clientConn, targetConn)
	<-errCh
}

type connResponseWriter struct {
	conn net.Conn
}

func newResponseWriter(conn net.Conn) connResponseWriter {
	return connResponseWriter{conn: conn}
}

func (w connResponseWriter) Header() http.Header {
	return make(http.Header)
}

func (w connResponseWriter) Write(data []byte) (int, error) {
	return w.conn.Write(data)
}

func (w connResponseWriter) WriteHeader(statusCode int) {
	fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", statusCode, http.StatusText(statusCode))
}

func startEchoServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo server: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func newHostRuleSet(t *testing.T, host string) rules.HostRuleSet {
	t.Helper()
	set, err := rules.ParseHostRules(strings.NewReader(host + "\n"))
	if err != nil {
		t.Fatalf("parse host rules: %v", err)
	}
	return set
}

func mustDoProxyRequest(t *testing.T, proxyAddress string, target string) (*http.Response, string) {
	t.Helper()

	client := &http.Client{
		Transport: &http.Transport{Proxy: mustProxyURL(t, proxyAddress)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("proxy request %s: %v", target, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp, string(body)
}

func mustOpenTunnel(t *testing.T, proxyAddress string, targetAddr string) net.Conn {
	t.Helper()

	proxyURL, err := url.Parse(proxyAddress)
	if err != nil {
		t.Fatalf("parse proxy address: %v", err)
	}
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr); err != nil {
		t.Fatalf("write CONNECT request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected CONNECT 200, got %d", resp.StatusCode)
	}
	return conn
}

func mustProxyURL(t *testing.T, rawURL string) func(*http.Request) (*url.URL, error) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	return http.ProxyURL(parsed)
}

func newTestLogger(writer io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func failingDialerForHost(t *testing.T, blockedHost string) DialContext {
	t.Helper()

	dialer := (&net.Dialer{}).DialContext
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("split host port in test dialer: %v", err)
		}
		if host == blockedHost {
			return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
		}
		return dialer(ctx, network, addr)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
