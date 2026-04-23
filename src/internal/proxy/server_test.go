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

func TestServerCONNECTViaUpstreamPreservesBufferedTunnelBytes(t *testing.T) {
	targetAddr, closeTarget := startGreetingServer(t, "hello")
	defer closeTarget()

	upstream := newPrefetchUpstream(t, map[string]string{targetAddr: targetAddr})
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

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	buffer := make([]byte, 5)
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatalf("read greeting payload: %v", err)
	}
	if string(buffer) != "hello" {
		t.Fatalf("unexpected greeting payload: %q", string(buffer))
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

func TestServerAutoDetectDoesNotTriggerOnNonTCPDialFailureInCONNECTPath(t *testing.T) {
	targetAddr, closeTarget := startEchoServer(t)
	defer closeTarget()

	recorder := &memoryRecorder{}
	upstream := newFakeUpstream(t, map[string]string{targetAddr: targetAddr})
	defer upstream.Close()

	blockedHost, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         upstream.Address(),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorder,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("non-tcp-failure-%s", blockedHost)
		},
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp, conn := mustOpenTunnelResponse(t, proxyServer.URL, targetAddr)
	defer conn.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected CONNECT 502, got %d", resp.StatusCode)
	}
	if recorder.Count() != 0 {
		t.Fatalf("expected no auto-detect record, got %d", recorder.Count())
	}
	if got := upstream.ConnectCount(targetAddr); got != 0 {
		t.Fatalf("expected no upstream CONNECT, got %d", got)
	}
}

func TestServerCONNECTAutoDetectRecordsHostAfterSuccessfulForwarding(t *testing.T) {
	targetAddr, closeTarget := startEchoServer(t)
	defer closeTarget()

	recorder := &memoryRecorder{}
	upstream := newFakeUpstream(t, map[string]string{targetAddr: targetAddr})
	defer upstream.Close()

	blockedHost, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         upstream.Address(),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorder,
		DialContext:           failingDialerForHost(t, blockedHost),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	conn := mustOpenTunnel(t, proxyServer.URL, targetAddr)
	if recorder.Count() != 0 {
		t.Fatalf("expected no record before forwarding completes, got %d", recorder.Count())
	}

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
	if err := conn.Close(); err != nil {
		t.Fatalf("close tunnel: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if recorder.Count() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if recorder.Count() != 1 || recorder.LastHost() != blockedHost {
		t.Fatalf("unexpected recorder state after forwarding: count=%d host=%q", recorder.Count(), recorder.LastHost())
	}
}

func TestServerCONNECTAutoDetectDoesNotRecordHostWhenForwardingFails(t *testing.T) {
	targetAddr, closeTarget := startReadThenCloseServer(t)
	defer closeTarget()

	recorder := &memoryRecorder{}
	upstream := newFakeUpstream(t, map[string]string{targetAddr: targetAddr})
	defer upstream.Close()

	blockedHost, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	server := NewServer(Options{
		Engine:                rules.NewEngine(),
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         upstream.Address(),
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    recorder,
		DialContext:           failingDialerForHost(t, blockedHost),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	conn := mustOpenTunnel(t, proxyServer.URL, targetAddr)
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}
	buffer := make([]byte, 4)
	if _, err := conn.Read(buffer); err == nil {
		t.Fatal("expected forwarding read failure")
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if recorder.Count() != 0 {
			t.Fatalf("expected no record when forwarding fails, got %d", recorder.Count())
		}
		time.Sleep(10 * time.Millisecond)
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

func TestServerHTTPForwardingStripsProxySensitiveHeaders(t *testing.T) {
	headerCh := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerCh <- r.Header.Clone()
		fmt.Fprint(w, "header-ok")
	}))
	defer target.Close()

	server := NewServer(Options{
		Engine: rules.NewEngine(),
		Logger: newTestLogger(io.Discard),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Proxy-Authorization", "Basic Zm9vOmJhcg==")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Connection", "Keep-Alive, X-Hop-Test")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("X-Hop-Test", "drop-me")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	headers := <-headerCh
	if got := headers.Get("Proxy-Authorization"); got != "" {
		t.Fatalf("expected Proxy-Authorization to be stripped, got %q", got)
	}
	if got := headers.Get("Proxy-Connection"); got != "" {
		t.Fatalf("expected Proxy-Connection to be stripped, got %q", got)
	}
	if got := headers.Get("Keep-Alive"); got != "" {
		t.Fatalf("expected Keep-Alive to be stripped, got %q", got)
	}
	if got := headers.Get("X-Hop-Test"); got != "" {
		t.Fatalf("expected Connection token header to be stripped, got %q", got)
	}
}

func TestServerHTTPDefaultTransportReusesUpstreamTunnel(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "shared-transport-ok")
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

	resp1, body1 := mustDoProxyRequest(t, proxyServer.URL, target.URL+"/first")
	defer resp1.Body.Close()
	resp2, body2 := mustDoProxyRequest(t, proxyServer.URL, target.URL+"/second")
	defer resp2.Body.Close()

	if body1 != "shared-transport-ok" || body2 != "shared-transport-ok" {
		t.Fatalf("unexpected bodies: %q %q", body1, body2)
	}
	if got := upstream.ConnectCount(targetURL.Host); got != 1 {
		t.Fatalf("expected shared transport to reuse upstream tunnel, got %d CONNECT calls", got)
	}
}

func TestServerHTTPDefaultTransportSwitchesPoolsAfterRuleRefresh(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "route-switch-ok")
	}))
	defer target.Close()

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	upstream := newFakeUpstream(t, map[string]string{targetURL.Host: targetURL.Host})
	defer upstream.Close()

	engine := rules.NewEngine()
	server := NewServer(Options{
		Engine:        engine,
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: upstream.Address(),
	})
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	resp1, body1 := mustDoProxyRequest(t, proxyServer.URL, target.URL+"/direct")
	defer resp1.Body.Close()
	if body1 != "route-switch-ok" {
		t.Fatalf("unexpected first body: %q", body1)
	}
	if got := upstream.ConnectCount(targetURL.Host); got != 0 {
		t.Fatalf("expected first request to stay direct, got %d upstream CONNECT calls", got)
	}

	engine.ReplaceCustomRules(newHostRuleSet(t, targetURL.Hostname()))

	resp2, body2 := mustDoProxyRequest(t, proxyServer.URL, target.URL+"/upstream")
	defer resp2.Body.Close()
	if body2 != "route-switch-ok" {
		t.Fatalf("unexpected second body: %q", body2)
	}
	if got := upstream.ConnectCount(targetURL.Host); got != 1 {
		t.Fatalf("expected rule refresh to force a new upstream CONNECT, got %d", got)
	}
}

func TestConnectViaUpstreamReturnsDialError(t *testing.T) {
	want := errors.New("dial upstream failed")
	server := NewServer(Options{
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: "127.0.0.1:1080",
		UpstreamDialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, want
		},
	})

	_, err := server.connectViaUpstream(context.Background(), "example.com:80")
	if !errors.Is(err, want) {
		t.Fatalf("expected dial error %v, got %v", want, err)
	}
}

func TestConnectViaUpstreamReturnsErrorOnNon200Response(t *testing.T) {
	upstreamAddr, closeUpstream := startScriptedUpstream(t, func(conn net.Conn) {
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
	})
	defer closeUpstream()

	server := NewServer(Options{
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: upstreamAddr,
	})

	_, err := server.connectViaUpstream(context.Background(), "example.com:80")
	if err == nil || !strings.Contains(err.Error(), "407 Proxy Authentication Required") {
		t.Fatalf("expected 407 CONNECT failure, got %v", err)
	}
}

func TestConnectViaUpstreamReturnsErrorOnTruncatedResponse(t *testing.T) {
	upstreamAddr, closeUpstream := startScriptedUpstream(t, func(conn net.Conn) {
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\nContent-Length")
	})
	defer closeUpstream()

	server := NewServer(Options{
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: upstreamAddr,
	})

	_, err := server.connectViaUpstream(context.Background(), "example.com:80")
	if err == nil {
		t.Fatal("expected truncated upstream response error")
	}
}

func TestConnectViaUpstreamRequiresConfiguredProxy(t *testing.T) {
	server := NewServer(Options{Logger: newTestLogger(io.Discard)})

	_, err := server.connectViaUpstream(context.Background(), "example.com:80")
	if err == nil || !strings.Contains(err.Error(), "upstream proxy is not configured") {
		t.Fatalf("expected missing upstream proxy error, got %v", err)
	}
}

func TestServeHTTPRejectsInvalidTargets(t *testing.T) {
	server := NewServer(Options{Logger: newTestLogger(io.Discard)})

	httpRecorder := httptest.NewRecorder()
	server.ServeHTTP(httpRecorder, &http.Request{Method: http.MethodGet, URL: &url.URL{}})
	if httpRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP bad request, got %d", httpRecorder.Code)
	}

	connectRecorder := httptest.NewRecorder()
	server.ServeHTTP(connectRecorder, &http.Request{Method: http.MethodConnect, URL: &url.URL{}})
	if connectRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected CONNECT bad request, got %d", connectRecorder.Code)
	}
}

func TestHandleConnectReturnsInternalErrorWhenHijackUnsupported(t *testing.T) {
	targetAddr, closeTarget := startEchoServer(t)
	defer closeTarget()

	server := NewServer(Options{
		Logger: newTestLogger(io.Discard),
	})

	req := &http.Request{
		Method: http.MethodConnect,
		Host:   targetAddr,
		URL:    &url.URL{Host: targetAddr},
	}
	recorder := httptest.NewRecorder()
	server.handleConnect(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when hijack unsupported, got %d", recorder.Code)
	}
}

func TestTargetHelpersNormalizeHostsAndPorts(t *testing.T) {
	httpReq := &http.Request{
		URL:  &url.URL{Scheme: "https", Host: "Example.COM"},
		Host: "ignored.example",
	}
	host, addr, err := httpTarget(httpReq)
	if err != nil {
		t.Fatalf("httpTarget returned error: %v", err)
	}
	if host != "example.com" || addr != "example.com:443" {
		t.Fatalf("unexpected https target: %q %q", host, addr)
	}

	connectReq := &http.Request{URL: &url.URL{}, RequestURI: "[2001:db8::1]"}
	host, addr, err = connectTarget(connectReq)
	if err != nil {
		t.Fatalf("connectTarget returned error: %v", err)
	}
	if host != "2001:db8::1" || addr != "[2001:db8::1]:443" {
		t.Fatalf("unexpected CONNECT target: %q %q", host, addr)
	}

	host, addr, err = splitTarget("Example.COM:8443", "80")
	if err != nil {
		t.Fatalf("splitTarget returned error: %v", err)
	}
	if host != "example.com" || addr != "example.com:8443" {
		t.Fatalf("unexpected split target: %q %q", host, addr)
	}

	if normalized := normalizeHost("[Example.COM.]"); normalized != "example.com" {
		t.Fatalf("unexpected normalized host: %q", normalized)
	}
	if port := defaultPortForScheme(" ftp ", "21"); port != "21" {
		t.Fatalf("unexpected fallback port: %q", port)
	}
}

func TestResolveDialContextUsesRequestScopedDialer(t *testing.T) {
	resolved := resolveDialContext(nil)
	if _, err := resolved(context.Background(), "tcp", "example.com:80"); err == nil {
		t.Fatal("expected missing request dial context error")
	}

	want := errors.New("request scoped dialer")
	ctx := withRequestDial(context.Background(), func(context.Context, string, string) (net.Conn, error) {
		return nil, want
	})
	if _, err := resolved(ctx, "tcp", "example.com:80"); !errors.Is(err, want) {
		t.Fatalf("expected request dialer error %v, got %v", want, err)
	}
}

func TestBufferedConnAndTunnelHelpers(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	conn := &halfCloseConn{Conn: left}
	buffered := newBufferedConn(conn, strings.NewReader("hi"))

	data, err := io.ReadAll(buffered)
	if err != nil {
		t.Fatalf("read buffered conn: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("unexpected buffered data: %q", string(data))
	}

	if err := buffered.(interface{ CloseRead() error }).CloseRead(); err != nil {
		t.Fatalf("close read: %v", err)
	}
	if err := buffered.(interface{ CloseWrite() error }).CloseWrite(); err != nil {
		t.Fatalf("close write: %v", err)
	}
	if conn.readClosed != 1 || conn.writeClosed != 1 {
		t.Fatalf("expected buffered conn to delegate close calls, got read=%d write=%d", conn.readClosed, conn.writeClosed)
	}

	closeWriter(conn)
	closeReader(conn)
	if conn.readClosed != 2 || conn.writeClosed != 2 {
		t.Fatalf("expected helper close calls, got read=%d write=%d", conn.readClosed, conn.writeClosed)
	}

	if tunnelForwardSucceeded() {
		t.Fatal("expected empty tunnel result to fail")
	}
	if tunnelForwardSucceeded(tunnelCopyResult{bytes: 1}, tunnelCopyResult{bytes: 0}) {
		t.Fatal("expected zero-byte tunnel result to fail")
	}
	if tunnelForwardSucceeded(tunnelCopyResult{bytes: 1}, tunnelCopyResult{bytes: 1, err: io.EOF}) {
		t.Fatal("expected tunnel error to fail")
	}
	if !tunnelForwardSucceeded(tunnelCopyResult{bytes: 1}, tunnelCopyResult{bytes: 2}) {
		t.Fatal("expected successful tunnel results to pass")
	}
}

func TestTargetHelpersRejectEmptyHostsAndAllowNoopHalfClose(t *testing.T) {
	if _, _, err := splitTarget("   ", "80"); err == nil {
		t.Fatal("expected splitTarget to reject empty host")
	}
	if normalized := normalizeHost(" Example.COM:443 "); normalized != "example.com" {
		t.Fatalf("unexpected normalized host with port: %q", normalized)
	}
	if normalizeHost("   ") != "" {
		t.Fatal("expected empty normalized host")
	}

	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	buffered := newBufferedConn(right, strings.NewReader(""))
	if err := buffered.(interface{ CloseRead() error }).CloseRead(); err != nil {
		t.Fatalf("expected noop CloseRead to succeed, got %v", err)
	}
	if err := buffered.(interface{ CloseWrite() error }).CloseWrite(); err != nil {
		t.Fatalf("expected noop CloseWrite to succeed, got %v", err)
	}
}

func TestNewServerAppliesDefaultLoggerAndIgnoresEmptyReset(t *testing.T) {
	server := NewServer(Options{})
	if server.logger == nil {
		t.Fatal("expected default logger to be created")
	}

	server.attempts["example.com"] = 2
	server.resetAttempts("")
	if got := server.attempts["example.com"]; got != 2 {
		t.Fatalf("expected attempts to remain unchanged, got %d", got)
	}
}

func TestResolveDialContextUsesProvidedFallback(t *testing.T) {
	want := errors.New("fallback dialer")
	resolved := resolveDialContext(func(context.Context, string, string) (net.Conn, error) {
		return nil, want
	})

	if _, err := resolved(context.Background(), "tcp", "example.com:80"); !errors.Is(err, want) {
		t.Fatalf("expected fallback error %v, got %v", want, err)
	}
}

func TestConnectionTokensSkipsEmptyValues(t *testing.T) {
	tokens := connectionTokens([]string{" keep-alive, , upgrade ,  "})
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0] != "Keep-Alive" || tokens[1] != "Upgrade" {
		t.Fatalf("unexpected tokens: %#v", tokens)
	}
}

func TestCloseHelpersIgnoreTargetsWithoutHalfClose(t *testing.T) {
	closeWriter(struct{}{})
	closeReader(struct{}{})
}

func TestSplitTargetRejectsMissingHosts(t *testing.T) {
	if _, _, err := splitTarget(":80", "443"); err == nil {
		t.Fatal("expected missing host error for host:port target")
	}
	if _, _, err := splitTarget("[]", "443"); err == nil {
		t.Fatal("expected missing host error for bracket target")
	}
}

func TestHandleHTTPLogsBodyCopyFailure(t *testing.T) {
	var logs bytes.Buffer
	server := NewServer(Options{
		Logger: newTestLogger(&logs),
		NewRoundTripper: func(DialContext) http.RoundTripper {
			return roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("payload")),
				}, nil
			})
		},
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	writer := &failingResponseWriter{header: make(http.Header), writeErr: errors.New("write failed")}
	server.handleHTTP(writer, req)

	if writer.code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", writer.code)
	}
	if !strings.Contains(logs.String(), "copy response body") {
		t.Fatalf("expected copy response body log, got %q", logs.String())
	}
}

func TestHandleConnectReturnsBadGatewayWhenHijackFails(t *testing.T) {
	targetConn := &stubConn{}
	server := NewServer(Options{
		Logger: newTestLogger(io.Discard),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return targetConn, nil
		},
	})

	req := &http.Request{
		Method: http.MethodConnect,
		Host:   "example.com:443",
		URL:    &url.URL{Host: "example.com:443"},
	}
	writer := &hijackableResponseWriter{
		header:    make(http.Header),
		hijackErr: errors.New("hijack failed"),
	}
	server.handleConnect(writer, req)

	if writer.code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", writer.code)
	}
	if !targetConn.closed {
		t.Fatal("expected target connection to be closed")
	}
}

func TestHandleConnectLogsClientWriteFailure(t *testing.T) {
	targetConn := &stubConn{}
	clientConn := &stubConn{writeErr: errors.New("write failed")}
	server := NewServer(Options{
		Logger: newTestLogger(io.Discard),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return targetConn, nil
		},
	})

	req := &http.Request{
		Method: http.MethodConnect,
		Host:   "example.com:443",
		URL:    &url.URL{Host: "example.com:443"},
	}
	writer := &hijackableResponseWriter{
		header:   make(http.Header),
		conn:     clientConn,
		buffered: bufio.NewReadWriter(bufio.NewReader(strings.NewReader("")), bufio.NewWriter(io.Discard)),
	}
	server.handleConnect(writer, req)

	if !clientConn.closed {
		t.Fatal("expected client connection to be closed")
	}
}

func TestHandleConnectLogsBufferedFlushFailure(t *testing.T) {
	var logs bytes.Buffer
	targetConn := &stubConn{writeErr: errors.New("flush failed")}
	clientConn := &stubConn{}
	reader := bufio.NewReader(strings.NewReader("hello"))
	if _, err := reader.Peek(len("hello")); err != nil {
		t.Fatalf("peek buffered data: %v", err)
	}

	server := NewServer(Options{
		Logger: newTestLogger(&logs),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return targetConn, nil
		},
	})

	req := &http.Request{
		Method: http.MethodConnect,
		Host:   "example.com:443",
		URL:    &url.URL{Host: "example.com:443"},
	}
	writer := &hijackableResponseWriter{
		header:   make(http.Header),
		conn:     clientConn,
		buffered: bufio.NewReadWriter(reader, bufio.NewWriter(io.Discard)),
	}
	server.handleConnect(writer, req)

	if !strings.Contains(logs.String(), "flush buffered CONNECT bytes") {
		t.Fatalf("expected buffered flush log, got %q", logs.String())
	}
}

func TestForwardHTTPRequestReturnsUpstreamFallbackError(t *testing.T) {
	var calls int
	server := NewServer(Options{
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         "127.0.0.1:1",
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    &memoryRecorder{},
		NewRoundTripper: func(DialContext) http.RoundTripper {
			return roundTripperFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
				}
				return nil, errors.New("upstream roundtrip failed")
			})
		},
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	resp, fromAutoDetect, err := server.forwardHTTPRequest(
		req,
		"example.com",
		"example.com:80",
		rules.Decision{Source: rules.DecisionSourceDefault},
		false,
	)

	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if fromAutoDetect {
		t.Fatal("expected auto-detect flag to remain false")
	}
	if err == nil || err.Error() != "upstream roundtrip failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenTunnelReturnsUpstreamFallbackError(t *testing.T) {
	server := NewServer(Options{
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         "127.0.0.1:1",
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 1,
		AutoDetectRecorder:    &memoryRecorder{},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
		},
		UpstreamDialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("upstream dial failed")
		},
	})

	conn, fromAutoDetect, err := server.openTunnel(
		context.Background(),
		"example.com",
		"example.com:443",
		rules.Decision{Source: rules.DecisionSourceDefault},
		false,
	)

	if conn != nil {
		t.Fatalf("expected nil connection, got %#v", conn)
	}
	if fromAutoDetect {
		t.Fatal("expected auto-detect flag to remain false")
	}
	if err == nil || err.Error() != "upstream dial failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenTunnelReturnsDirectErrorBeforeAutoDetectThreshold(t *testing.T) {
	server := NewServer(Options{
		Logger:                newTestLogger(io.Discard),
		UpstreamProxy:         "127.0.0.1:1",
		AutoDetectEnabled:     true,
		AutoDetectMaxAttempts: 2,
		AutoDetectRecorder:    &memoryRecorder{},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
		},
		UpstreamDialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("expected upstream dial to stay below threshold")
			return nil, nil
		},
	})

	conn, fromAutoDetect, err := server.openTunnel(
		context.Background(),
		"example.com",
		"example.com:443",
		rules.Decision{Source: rules.DecisionSourceDefault},
		false,
	)

	if conn != nil {
		t.Fatalf("expected nil connection, got %#v", conn)
	}
	if fromAutoDetect {
		t.Fatal("expected auto-detect flag to remain false")
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected dial error, got %v", err)
	}
	if got := server.attempts["example.com"]; got != 1 {
		t.Fatalf("expected one recorded failure, got %d", got)
	}
}

func TestConnectViaUpstreamReturnsRequestWriteError(t *testing.T) {
	upstreamConn := &stubConn{writeErr: errors.New("write failed")}
	server := NewServer(Options{
		Logger:        newTestLogger(io.Discard),
		UpstreamProxy: "127.0.0.1:1",
		UpstreamDialContext: func(context.Context, string, string) (net.Conn, error) {
			return upstreamConn, nil
		},
	})

	_, err := server.connectViaUpstream(context.Background(), "example.com:443")
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("unexpected error: %v", err)
	}
	if !upstreamConn.closed {
		t.Fatal("expected upstream connection to be closed")
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

type halfCloseConn struct {
	net.Conn
	readClosed  int
	writeClosed int
}

func (c *halfCloseConn) CloseRead() error {
	c.readClosed++
	return nil
}

func (c *halfCloseConn) CloseWrite() error {
	c.writeClosed++
	return nil
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

func startScriptedUpstream(t *testing.T, handler func(net.Conn)) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen scripted upstream: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()

	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
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

func startGreetingServer(t *testing.T, greeting string) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen greeting server: %v", err)
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
				_, _ = io.WriteString(c, greeting)
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func startClosingServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen closing server: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func startReadThenCloseServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen read-then-close server: %v", err)
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
				buffer := make([]byte, 32)
				_, _ = c.Read(buffer)
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

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected CONNECT 200, got %d", resp.StatusCode)
	}
	return newBufferedConn(conn, reader)
}

func mustOpenTunnelResponse(t *testing.T, proxyAddress string, targetAddr string) (*http.Response, net.Conn) {
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

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	return resp, newBufferedConn(conn, reader)
}

func TestMustOpenTunnelPreservesBufferedHandshakeBytes(t *testing.T) {
	const payload = "hello-after-connect"
	proxyAddress, closeProxy := startScriptedUpstream(t, func(conn net.Conn) {
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			t.Errorf("read CONNECT request: %v", err)
			return
		}
		if req.Method != http.MethodConnect {
			t.Errorf("unexpected method: %s", req.Method)
			return
		}

		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"+payload); err != nil {
			t.Errorf("write handshake response: %v", err)
		}
	})
	defer closeProxy()

	conn := mustOpenTunnel(t, "http://"+proxyAddress, "example.com:443")
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	buffer := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatalf("read buffered payload: %v", err)
	}
	if string(buffer) != payload {
		t.Fatalf("unexpected payload: %q", string(buffer))
	}
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

type failingResponseWriter struct {
	header   http.Header
	code     int
	writeErr error
}

func (w *failingResponseWriter) Header() http.Header {
	return w.header
}

func (w *failingResponseWriter) Write(data []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(data), nil
}

func (w *failingResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

type hijackableResponseWriter struct {
	header    http.Header
	code      int
	conn      net.Conn
	buffered  *bufio.ReadWriter
	hijackErr error
}

func (w *hijackableResponseWriter) Header() http.Header {
	return w.header
}

func (w *hijackableResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *hijackableResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func (w *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.hijackErr != nil {
		return nil, nil, w.hijackErr
	}
	return w.conn, w.buffered, nil
}

type stubConn struct {
	writeErr error
	closed   bool
	buffer   bytes.Buffer
}

func (c *stubConn) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (c *stubConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return c.buffer.Write(p)
}

func (c *stubConn) Close() error {
	c.closed = true
	return nil
}

func (c *stubConn) LocalAddr() net.Addr {
	return stubAddr("local")
}

func (c *stubConn) RemoteAddr() net.Addr {
	return stubAddr("remote")
}

func (c *stubConn) SetDeadline(time.Time) error {
	return nil
}

func (c *stubConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *stubConn) SetWriteDeadline(time.Time) error {
	return nil
}

type stubAddr string

func (a stubAddr) Network() string {
	return "tcp"
}

func (a stubAddr) String() string {
	return string(a)
}

type prefetchUpstream struct {
	t        *testing.T
	listener net.Listener
	routes   map[string]string
	closed   atomic.Bool
}

func newPrefetchUpstream(t *testing.T, routes map[string]string) *prefetchUpstream {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen prefetch upstream: %v", err)
	}

	u := &prefetchUpstream{
		t:        t,
		listener: listener,
		routes:   routes,
	}
	go u.serve()
	return u
}

func (u *prefetchUpstream) Address() string {
	return u.listener.Addr().String()
}

func (u *prefetchUpstream) Close() {
	if u.closed.CompareAndSwap(false, true) {
		_ = u.listener.Close()
	}
}

func (u *prefetchUpstream) serve() {
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

func (u *prefetchUpstream) handleConn(clientConn net.Conn) {
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
		fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}

	targetConn, err := net.Dial("tcp", actualAddr)
	if err != nil {
		fmt.Fprintf(clientConn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer targetConn.Close()

	if _, err := io.Copy(clientConn, io.MultiReader(
		strings.NewReader("HTTP/1.1 200 Connection Established\r\n\r\n"),
		targetConn,
	)); err != nil && !errors.Is(err, net.ErrClosed) {
		return
	}
}
