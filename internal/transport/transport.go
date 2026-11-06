// Package transport performs the actual HTTP/SOAP request and normalises the
// response for the frontend.
package transport

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"soapweb/internal/charset"
	"soapweb/internal/model"
)

// Send executes an HTTP request described by req, honouring the project's proxy
// settings, and returns a normalised response. Network-level failures are
// reported inside model.SendResponse.Error so the frontend can render them
// uniformly.
func Send(req model.SendRequest, proxyCfg *model.ProxySettings) model.SendResponse {
	out := model.SendResponse{Headers: map[string]string{}}

	if err := proxyCfg.Validate(); err != nil {
		out.Error = "代理配置无效: " + err.Error()
		return out
	}

	method := req.Method
	if method == "" {
		method = http.MethodPost
	}

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = bytes.NewBufferString(req.Body)
	}

	httpReq, err := http.NewRequest(method, req.URL, bodyReader)
	if err != nil {
		out.Error = "构造请求失败: " + err.Error()
		return out
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	// Apply content type last so an explicit value wins over a header map entry.
	if req.ContentType != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}

	client, err := buildClient(proxyCfg, timeout)
	if err != nil {
		out.Error = "代理配置错误: " + err.Error()
		return out
	}

	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		out.DurationMS = time.Since(start).Milliseconds()
		out.Error = "请求失败: " + err.Error()
		return out
	}
	defer resp.Body.Close()

	// Snapshot status and headers before reading the body: if the read fails
	// (timeout mid-stream, truncated connection) the caller still needs the
	// status line and headers to understand what happened.
	out.Status = resp.StatusCode
	out.StatusText = http.StatusText(resp.StatusCode)
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			out.Headers[k] = vals[0]
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	out.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		// Keep whatever arrived so a truncated response is still inspectable.
		out.Body = decodeBody(body, resp.Header.Get("Content-Type"))
		out.Size = len(body)
		out.Error = "读取响应失败: " + err.Error()
		return out
	}

	out.Body = decodeBody(body, resp.Header.Get("Content-Type"))
	out.Size = len(body)
	if len(body) >= maxBodyBytes {
		out.Truncated = true
	}
	return out
}

// maxBodyBytes caps how much of a response we read. Debugging payloads are
// small; an unbounded io.ReadAll on a runaway endpoint would allocate gigabytes
// and then be copied again into a string for JSON encoding.
const maxBodyBytes = 16 << 20 // 16 MB

// decodeBody converts the response body to UTF-8.
//
// Legacy Chinese services commonly answer with charset=GBK (or declare
// encoding="GBK" in the XML declaration) while the bytes themselves are not
// UTF-8. Reading them as UTF-8 produces mojibake, so the charset is resolved
// from the Content-Type header first, then from the XML declaration.
func decodeBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}
	// The Content-Type header is authoritative when it names a charset.
	if cs := charsetFromContentType(contentType); cs != "" {
		return string(charset.Decode(body, cs))
	}
	// Otherwise fall back to the document's own XML declaration.
	return string(charset.DecodeXML(body))
}

// charsetFromContentType extracts charset= from a Content-Type header value.
func charsetFromContentType(contentType string) string {
	for _, part := range strings.Split(contentType, ";") {
		part = strings.TrimSpace(part)
		if len(part) < 8 || !strings.EqualFold(part[:8], "charset=") {
			continue
		}
		return strings.Trim(part[8:], `"' `)
	}
	return ""
}

// transports caches one *http.Transport per proxy configuration.
//
// Allocating a fresh Transport per request leaked three goroutines every time
// (readLoop, writeLoop, persistConn) because the previous Transport was never
// closed and its idle connections were never reaped. Debugging a service means
// sending many requests, so the leak grew without bound. Transports are now
// shared and their idle connections are reused.
var transports = struct {
	mu sync.Mutex
	m  map[string]*http.Transport
}{m: map[string]*http.Transport{}}

// buildClient returns a client honouring the proxy settings and timeout.
// The underlying Transport is shared; only the per-request timeout varies.
func buildClient(cfg *model.ProxySettings, timeout time.Duration) (*http.Client, error) {
	tr, err := transportFor(cfg)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// transportFor returns the shared Transport for cfg, creating it on first use.
func transportFor(cfg *model.ProxySettings) (*http.Transport, error) {
	key := transportKey(cfg)

	transports.mu.Lock()
	defer transports.mu.Unlock()
	if tr, ok := transports.m[key]; ok {
		return tr, nil
	}

	tr := &http.Transport{
		Proxy:               nil,
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}

	mode := "system"
	if cfg != nil && cfg.Mode != "" {
		mode = cfg.Mode
	}

	switch mode {
	case "direct":
		// Proxy stays nil → always direct.
	case "system":
		tr.Proxy = http.ProxyFromEnvironment
	case "custom":
		addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
		switch cfg.Type {
		case "http":
			u := &url.URL{Scheme: "http", Host: addr}
			if cfg.Username != "" || cfg.Password != "" {
				u.User = url.UserPassword(cfg.Username, cfg.Password)
			}
			tr.Proxy = http.ProxyURL(u)
		case "socks5":
			var auth *proxy.Auth
			if cfg.Username != "" || cfg.Password != "" {
				auth = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
			}
			dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
			if err != nil {
				return nil, err
			}
			tr.Proxy = nil // SOCKS5 is handled at the dialer level.
			tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if cd, ok := dialer.(proxy.ContextDialer); ok {
					return cd.DialContext(ctx, network, address)
				}
				return dialer.Dial(network, address)
			}
		default:
			return nil, &proxyConfigError{"未知的代理类型: " + cfg.Type}
		}
	default:
		return nil, &proxyConfigError{"未知的代理模式: " + mode}
	}

	transports.m[key] = tr
	return tr, nil
}

// transportKey identifies a proxy configuration for the Transport cache.
func transportKey(cfg *model.ProxySettings) string {
	if cfg == nil {
		return "system|"
	}
	return cfg.Mode + "|" + cfg.Type + "|" + cfg.Host + "|" +
		strconv.Itoa(cfg.Port) + "|" + cfg.Username + "|" + cfg.Password
}

// CloseIdleConnections drops pooled connections held by every cached
// Transport. Used by tests and on shutdown.
func CloseIdleConnections() {
	transports.mu.Lock()
	defer transports.mu.Unlock()
	for _, tr := range transports.m {
		tr.CloseIdleConnections()
	}
}

type proxyConfigError struct{ msg string }

func (e *proxyConfigError) Error() string { return e.msg }

// HeaderKeys returns sorted header names (helpers for deterministic tests).
func HeaderKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
