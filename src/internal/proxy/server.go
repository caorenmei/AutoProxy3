package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"sync"

	"github.com/caorenmei/autoproxy3/src/internal/rules"
)

// DialContext 表示可注入的拨号函数。
//
// 代理层通过该类型抽象直连与上游代理连接过程，以便在测试中替换网络依赖。
type DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

// RoundTripperFactory 表示创建 HTTP 转发器的工厂函数。
//
// 调用方可基于拨号函数注入自定义 RoundTripper，以便测试直连与经上游隧道转发逻辑。
type RoundTripperFactory func(dial DialContext) http.RoundTripper

// AutoDetectRecorder 定义自动探测结果持久化接口。
//
// 实现方负责将成功探测出的目标主机写入外部存储；代理层仅调用该接口，
// 不直接触碰文件系统。
type AutoDetectRecorder interface {
	Record(ctx context.Context, host string) error
}

// Options 表示代理服务初始化参数。
//
// 该配置聚合规则引擎、日志、拨号器、上游代理与自动探测组件。
// 未提供的可选项会在 NewServer 中补充安全默认值。
type Options struct {
	Engine                *rules.Engine
	Logger                *slog.Logger
	UpstreamProxy         string
	AutoDetectEnabled     bool
	AutoDetectMaxAttempts int
	AutoDetectRecorder    AutoDetectRecorder
	DialContext           DialContext
	UpstreamDialContext   DialContext
	NewRoundTripper       RoundTripperFactory
}

// Server 表示代理主流程处理器。
//
// Server 同时支持普通 HTTP 代理请求与 CONNECT 隧道请求，并根据规则引擎结果
// 在直连、上游 CONNECT 转发以及自动探测回退之间做出选择。
type Server struct {
	engine              *rules.Engine
	logger              *slog.Logger
	upstreamProxy       string
	autoDetectEnabled   bool
	autoDetectMaxCounts int
	autoDetectRecorder  AutoDetectRecorder
	dialContext         DialContext
	upstreamDialContext DialContext
	newRoundTripper     RoundTripperFactory
	sharedRoundTripper  http.RoundTripper

	attemptMu sync.Mutex
	attempts  map[string]int
}

// NewServer 创建并返回代理服务处理器。
//
// 返回值可直接作为 http.Handler 使用。函数会为日志、拨号器与 HTTP 转发器补充默认实现；
// 若未提供规则引擎，则创建一个默认直连的空引擎。
func NewServer(opts Options) *Server {
	engine := opts.Engine
	if engine == nil {
		engine = rules.NewEngine()
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	dialContext := opts.DialContext
	if dialContext == nil {
		dialContext = (&net.Dialer{}).DialContext
	}

	upstreamDialContext := opts.UpstreamDialContext
	if upstreamDialContext == nil {
		upstreamDialContext = (&net.Dialer{}).DialContext
	}

	newRoundTripper := opts.NewRoundTripper
	var sharedRoundTripper http.RoundTripper
	if newRoundTripper == nil {
		newRoundTripper = defaultRoundTripper
		sharedRoundTripper = newRoundTripper(nil)
	}

	maxAttempts := opts.AutoDetectMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	return &Server{
		engine:              engine,
		logger:              logger,
		upstreamProxy:       strings.TrimSpace(opts.UpstreamProxy),
		autoDetectEnabled:   opts.AutoDetectEnabled,
		autoDetectMaxCounts: maxAttempts,
		autoDetectRecorder:  opts.AutoDetectRecorder,
		dialContext:         dialContext,
		upstreamDialContext: upstreamDialContext,
		newRoundTripper:     newRoundTripper,
		sharedRoundTripper:  sharedRoundTripper,
		attempts:            make(map[string]int),
	}
}

// ServeHTTP 处理入站代理请求。
//
// 当请求方法为 CONNECT 时，函数会建立双向隧道；否则按普通 HTTP 代理请求转发。
// 若转发失败，函数会向客户端返回 502，并将详细错误写入日志。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Method, http.MethodConnect) {
		s.handleConnect(w, r)
		return
	}
	s.handleHTTP(w, r)
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	targetHost, targetAddr, err := httpTarget(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	decision := s.engine.Decide(r.URL.String())
	useUpstream := s.resolveProxyUsage(decision, targetHost)

	resp, fromAutoDetect, err := s.forwardHTTPRequest(r, targetHost, targetAddr, decision, useUpstream)
	if err != nil {
		s.logger.Error("forward http request", "target", targetAddr, "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		s.logger.Error("copy response body", "target", targetAddr, "error", err)
		return
	}

	if fromAutoDetect {
		s.persistAutoDetectHost(r.Context(), targetHost)
	}
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	targetHost, targetAddr, err := connectTarget(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	decision := s.engine.DecideHost(targetHost)
	useUpstream := s.resolveProxyUsage(decision, targetHost)

	targetConn, fromAutoDetect, err := s.openTunnel(r.Context(), targetHost, targetAddr, decision, useUpstream)
	if err != nil {
		s.logger.Error("open CONNECT tunnel", "target", targetAddr, "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking is not supported", http.StatusInternalServerError)
		return
	}

	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		s.logger.Error("hijack client connection", "target", targetAddr, "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer clientConn.Close()

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		s.logger.Error("write CONNECT response", "target", targetAddr, "error", err)
		return
	}

	if bufferedCount := buffered.Reader.Buffered(); bufferedCount > 0 {
		if _, err := io.CopyN(targetConn, buffered, int64(bufferedCount)); err != nil {
			s.logger.Error("flush buffered CONNECT bytes", "target", targetAddr, "error", err)
			return
		}
	}

	errCh := make(chan tunnelCopyResult, 2)
	go proxyTunnel(errCh, targetConn, clientConn)
	go proxyTunnel(errCh, clientConn, targetConn)

	resultA := <-errCh
	resultB := <-errCh
	if fromAutoDetect && tunnelForwardSucceeded(resultA, resultB) {
		s.persistAutoDetectHost(r.Context(), targetHost)
	}
}

func (s *Server) forwardHTTPRequest(r *http.Request, targetHost, targetAddr string, decision rules.Decision, useUpstream bool) (*http.Response, bool, error) {
	resp, err := s.roundTrip(r, targetAddr, useUpstream)
	if err == nil {
		if decision.Source == rules.DecisionSourceDefault {
			s.resetAttempts(targetHost)
		}
		return resp, false, nil
	}

	if !s.shouldAutoDetect(decision, targetHost, useUpstream) || !isTCPDialFailure(err) {
		return nil, false, err
	}

	attempts := s.recordFailure(targetHost)
	if attempts < s.autoDetectMaxCounts || s.upstreamProxy == "" {
		return nil, false, err
	}

	resp, upstreamErr := s.roundTrip(r, targetAddr, true)
	if upstreamErr != nil {
		return nil, false, upstreamErr
	}
	s.resetAttempts(targetHost)
	return resp, true, nil
}

func (s *Server) openTunnel(ctx context.Context, targetHost, targetAddr string, decision rules.Decision, useUpstream bool) (net.Conn, bool, error) {
	conn, err := s.dialTarget(ctx, targetAddr, useUpstream)
	if err == nil {
		if decision.Source == rules.DecisionSourceDefault {
			s.resetAttempts(targetHost)
		}
		return conn, false, nil
	}

	if !s.shouldAutoDetect(decision, targetHost, useUpstream) || !isTCPDialFailure(err) {
		return nil, false, err
	}

	attempts := s.recordFailure(targetHost)
	if attempts < s.autoDetectMaxCounts || s.upstreamProxy == "" {
		return nil, false, err
	}

	conn, upstreamErr := s.dialTarget(ctx, targetAddr, true)
	if upstreamErr != nil {
		return nil, false, upstreamErr
	}
	s.resetAttempts(targetHost)
	return conn, true, nil
}

func (s *Server) roundTrip(r *http.Request, targetAddr string, useUpstream bool) (*http.Response, error) {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		_ = network
		return s.dialTarget(ctx, targetAddr, useUpstream)
	}

	request := cloneRequest(r)
	request = request.WithContext(withRequestDial(request.Context(), dial))
	if s.sharedRoundTripper != nil {
		return s.sharedRoundTripper.RoundTrip(request)
	}
	return s.newRoundTripper(dial).RoundTrip(request)
}

func (s *Server) dialTarget(ctx context.Context, targetAddr string, useUpstream bool) (net.Conn, error) {
	if useUpstream {
		return s.connectViaUpstream(ctx, targetAddr)
	}
	return s.dialContext(ctx, "tcp", targetAddr)
}

func (s *Server) connectViaUpstream(ctx context.Context, targetAddr string) (net.Conn, error) {
	if s.upstreamProxy == "" {
		return nil, errors.New("upstream proxy is not configured")
	}

	conn, err := s.upstreamDialContext(ctx, "tcp", s.upstreamProxy)
	if err != nil {
		return nil, err
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr); err != nil {
		_ = conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("upstream CONNECT failed: %s", resp.Status)
	}
	_ = resp.Body.Close()
	return newBufferedConn(conn, reader), nil
}

func (s *Server) resolveProxyUsage(decision rules.Decision, targetHost string) bool {
	if !decision.UseProxy {
		return false
	}
	if s.upstreamProxy != "" {
		return true
	}

	s.logger.Warn("upstream proxy is not configured, falling back to direct", "host", targetHost)
	return false
}

func (s *Server) shouldAutoDetect(decision rules.Decision, targetHost string, useUpstream bool) bool {
	return s.autoDetectEnabled && s.autoDetectRecorder != nil && !useUpstream && s.upstreamProxy != "" && decision.Source == rules.DecisionSourceDefault && targetHost != ""
}

func isTCPDialFailure(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}

	return opErr.Op == "dial" && strings.HasPrefix(opErr.Net, "tcp")
}

func (s *Server) persistAutoDetectHost(ctx context.Context, host string) {
	if err := s.autoDetectRecorder.Record(ctx, host); err != nil {
		s.logger.Error("record auto-detect host", "host", host, "error", err)
		return
	}
	s.engine.AddAutoDetectHost(host)
}

func (s *Server) recordFailure(host string) int {
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	s.attempts[host]++
	return s.attempts[host]
}

func (s *Server) resetAttempts(host string) {
	if host == "" {
		return
	}
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	delete(s.attempts, host)
}

func defaultRoundTripper(dial DialContext) http.RoundTripper {
	return &http.Transport{
		Proxy:               nil,
		DialContext:         resolveDialContext(dial),
		ForceAttemptHTTP2:   false,
		DisableCompression:  true,
		MaxIdleConnsPerHost: 1,
	}
}

func cloneRequest(r *http.Request) *http.Request {
	cloned := r.Clone(r.Context())
	cloned.RequestURI = ""
	cloned.Host = r.Host
	cloned.Header = r.Header.Clone()
	stripProxyRequestHeaders(cloned.Header)
	return cloned
}

func httpTarget(r *http.Request) (string, string, error) {
	host := strings.TrimSpace(r.URL.Host)
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return "", "", errors.New("missing target host")
	}
	return splitTarget(host, defaultPortForScheme(r.URL.Scheme, "80"))
}

func connectTarget(r *http.Request) (string, string, error) {
	target := strings.TrimSpace(r.Host)
	if target == "" {
		target = strings.TrimSpace(r.URL.Host)
	}
	if target == "" {
		target = strings.TrimSpace(r.RequestURI)
	}
	if target == "" {
		return "", "", errors.New("missing CONNECT target")
	}
	return splitTarget(target, "443")
}

func splitTarget(target, defaultPort string) (string, string, error) {
	if host, port, err := net.SplitHostPort(target); err == nil {
		normalized := normalizeHost(host)
		if normalized == "" {
			return "", "", errors.New("missing target host")
		}
		return normalized, net.JoinHostPort(normalized, port), nil
	}

	if strings.HasPrefix(target, "[") && strings.HasSuffix(target, "]") {
		normalized := normalizeHost(strings.Trim(target, "[]"))
		if normalized == "" {
			return "", "", errors.New("missing target host")
		}
		return normalized, net.JoinHostPort(normalized, defaultPort), nil
	}

	normalized := normalizeHost(target)
	if normalized == "" {
		return "", "", errors.New("missing target host")
	}
	return normalized, net.JoinHostPort(normalized, defaultPort), nil
}

func normalizeHost(target string) string {
	parsed := strings.TrimSpace(target)
	if parsed == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(parsed); err == nil {
		parsed = host
	}
	if strings.HasPrefix(parsed, "[") && strings.HasSuffix(parsed, "]") {
		parsed = strings.Trim(parsed, "[]")
	}
	return strings.TrimSuffix(strings.ToLower(parsed), ".")
}

func defaultPortForScheme(scheme, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return fallback
	}
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func proxyCopy(errCh chan<- error, dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	errCh <- err
}

type requestDialKey struct{}

type bufferedConn struct {
	net.Conn
	reader io.Reader
}

func newBufferedConn(conn net.Conn, reader io.Reader) net.Conn {
	return &bufferedConn{
		Conn:   conn,
		reader: reader,
	}
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *bufferedConn) CloseRead() error {
	closer, ok := c.Conn.(interface{ CloseRead() error })
	if !ok {
		return nil
	}
	return closer.CloseRead()
}

func (c *bufferedConn) CloseWrite() error {
	closer, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return nil
	}
	return closer.CloseWrite()
}

type tunnelCopyResult struct {
	bytes int64
	err   error
}

func proxyTunnel(errCh chan<- tunnelCopyResult, dst io.Writer, src io.Reader) {
	written, err := io.Copy(dst, src)
	closeWriter(dst)
	closeReader(src)
	errCh <- tunnelCopyResult{
		bytes: written,
		err:   err,
	}
}

func tunnelForwardSucceeded(results ...tunnelCopyResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if result.err != nil {
			return false
		}
		if result.bytes == 0 {
			return false
		}
	}
	return true
}

func withRequestDial(ctx context.Context, dial DialContext) context.Context {
	return context.WithValue(ctx, requestDialKey{}, dial)
}

func resolveDialContext(fallback DialContext) DialContext {
	if fallback != nil {
		return fallback
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dial, _ := ctx.Value(requestDialKey{}).(DialContext)
		if dial == nil {
			return nil, errors.New("missing request dial context")
		}
		return dial(ctx, network, addr)
	}
}

func stripProxyRequestHeaders(header http.Header) {
	tokens := connectionTokens(header.Values("Connection"))

	header.Del("Proxy-Authorization")
	header.Del("Proxy-Connection")

	for _, key := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(key)
	}

	for _, token := range tokens {
		header.Del(token)
	}
}

func connectionTokens(values []string) []string {
	tokens := make([]string, 0, len(values))
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			trimmed := textproto.TrimString(token)
			if trimmed == "" {
				continue
			}
			tokens = append(tokens, textproto.CanonicalMIMEHeaderKey(trimmed))
		}
	}
	return tokens
}

func closeWriter(target any) {
	closer, ok := target.(interface{ CloseWrite() error })
	if !ok {
		return
	}
	_ = closer.CloseWrite()
}

func closeReader(target any) {
	closer, ok := target.(interface{ CloseRead() error })
	if !ok {
		return
	}
	_ = closer.CloseRead()
}
