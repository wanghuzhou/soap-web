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
	"time"

	"golang.org/x/net/proxy"

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
	out.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		out.Error = "请求失败: " + err.Error()
		return out
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		out.Error = "读取响应失败: " + err.Error()
		out.Status = resp.StatusCode
		out.StatusText = http.StatusText(resp.StatusCode)
		return out
	}

	out.Status = resp.StatusCode
	out.StatusText = http.StatusText(resp.StatusCode)
	out.Body = string(body)
	out.Size = len(body)
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			out.Headers[k] = vals[0]
		}
	}
	return out
}

// buildClient constructs an http.Client whose transport follows the project's
// proxy settings.
func buildClient(cfg *model.ProxySettings, timeout time.Duration) (*http.Client, error) {
	tr := &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: timeout,
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

	return &http.Client{Transport: tr, Timeout: timeout}, nil
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
