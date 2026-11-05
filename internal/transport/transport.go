// Package transport performs the actual HTTP/SOAP request and normalises the
// response for the frontend.
package transport

import (
	"bytes"
	"io"
	"net/http"
	"sort"
	"time"

	"soapweb/internal/model"
)

// Send executes an HTTP request described by req and returns a normalised
// response. Network-level failures are reported inside model.SendResponse.Error
// so the frontend can render them uniformly.
func Send(req model.SendRequest) model.SendResponse {
	out := model.SendResponse{Headers: map[string]string{}}

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

	client := &http.Client{Timeout: timeout}
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
		out.StatusText = resp.Status
		return out
	}

	out.Status = resp.StatusCode
	out.StatusText = resp.Status
	out.Body = string(body)
	out.Size = len(body)
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			out.Headers[k] = vals[0]
		}
	}
	return out
}

// HeaderKeys returns sorted header names (helpers for deterministic tests).
func HeaderKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
