package transport

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"soapweb/internal/model"
)

// gbkBody 是 GBK 编码的 XML 响应："<r>中文测试</r>"。
var gbkBody = []byte{
	'<', 'r', '>',
	0xd6, 0xd0, 0xce, 0xc4, 0xb2, 0xe2, 0xca, 0xd4,
	'<', '/', 'r', '>',
}

// TestSendDecodesGBKFromContentType 回归：GBK 响应体曾按 UTF-8 解释，
// 中文全部变成乱码。charset 在 Content-Type 中时必须解码。
func TestSendDecodesGBKFromContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=GBK")
		_, _ = w.Write(gbkBody)
	}))
	defer srv.Close()

	resp := Send(model.SendRequest{URL: srv.URL, Method: "POST"}, nil)
	if resp.Error != "" {
		t.Fatalf("请求失败: %s", resp.Error)
	}
	if want := "<r>中文测试</r>"; resp.Body != want {
		t.Fatalf("body = %q, want %q", resp.Body, want)
	}
}

// TestSendDecodesGBKFromXMLDecl 回归：Content-Type 未声明 charset 时，
// 应退回到文档自身的 XML 声明。
func TestSendDecodesGBKFromXMLDecl(t *testing.T) {
	decl := []byte(`<?xml version="1.0" encoding="GBK"?><r>中文测试</r>`)
	// 把 "中文测试" 换成真实 GBK 字节。
	decl = append([]byte(`<?xml version="1.0" encoding="GBK"?><r>`), gbkBody[3:len(gbkBody)-4]...)
	decl = append(decl, []byte(`</r>`)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml") // 无 charset
		_, _ = w.Write(decl)
	}))
	defer srv.Close()

	resp := Send(model.SendRequest{URL: srv.URL, Method: "POST"}, nil)
	if resp.Error != "" {
		t.Fatalf("请求失败: %s", resp.Error)
	}
	if want := `<?xml version="1.0" encoding="GBK"?><r>中文测试</r>`; resp.Body != want {
		t.Fatalf("body = %q, want %q", resp.Body, want)
	}
}

// TestSendKeepsUTF8Body 确保 UTF-8 响应不被误转码。
func TestSendKeepsUTF8Body(t *testing.T) {
	const want = `<r>中文测试</r>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte(want))
	}))
	defer srv.Close()

	resp := Send(model.SendRequest{URL: srv.URL, Method: "POST"}, nil)
	if resp.Body != want {
		t.Fatalf("body = %q, want %q", resp.Body, want)
	}
}

// TestSendPreservesPartialBodyOnTimeout 回归：读体超时时，
// 已收到的内容、状态码与 headers 都必须保留，耗时也必须反映真实值。
func TestSendPreservesPartialBodyOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Trace", "abc")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial-data"))
		w.(http.Flusher).Flush()
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	start := time.Now()
	resp := Send(model.SendRequest{URL: srv.URL, Method: "POST", TimeoutMS: 100}, nil)
	elapsed := time.Since(start)

	if resp.Error == "" {
		t.Fatalf("期望超时错误，实际没有错误")
	}
	if resp.Status != http.StatusOK {
		t.Errorf("超时时 status 应保留 = %d", resp.Status)
	}
	if resp.Headers["X-Trace"] != "abc" {
		t.Errorf("超时时 headers 应保留，实际 %v", resp.Headers)
	}
	if resp.Body == "" {
		t.Errorf("超时时应保留已收到的部分响应体，实际为空")
	}
	// 耗时必须是真实值（此前固定为 Do() 返回时刻，显示 2ms）。
	if resp.DurationMS < 50 {
		t.Errorf("DurationMS = %d，应接近真实耗时 %v", resp.DurationMS, elapsed)
	}
}

// TestSendCapsBodySize 回归：响应体此前无上限，64MB 响应会分配数百 MB。
func TestSendCapsBodySize(t *testing.T) {
	const chunk = 1 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		buf := make([]byte, chunk)
		for i := range buf {
			buf[i] = 'A'
		}
		for i := 0; i < 20; i++ { // 20 MB，超过 16 MB 上限
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	resp := Send(model.SendRequest{URL: srv.URL, Method: "POST", TimeoutMS: 10000}, nil)
	if resp.Error != "" {
		t.Fatalf("请求失败: %s", resp.Error)
	}
	if !resp.Truncated {
		t.Errorf("超过上限的响应应标记 Truncated=true")
	}
	if resp.Size > maxBodyBytes {
		t.Errorf("Size = %d，应不超过上限 %d", resp.Size, maxBodyBytes)
	}
}

// TestSendReusesTransport 回归：每次 Send 新建 Transport 曾导致
// 每次泄漏 3 个 goroutine（60 次请求后 3 → 183）。
func TestSendReusesTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	base := runtime.NumGoroutine()
	for i := 0; i < 60; i++ {
		resp := Send(model.SendRequest{URL: srv.URL, Method: "POST"}, nil)
		if resp.Error != "" {
			t.Fatalf("第 %d 次请求失败: %s", i, resp.Error)
		}
	}
	// 给连接回收一点时间，但远小于 IdleConnTimeout。
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// 复用后应稳定在少量连接池 goroutine；允许适度抖动。
	if after > base+20 {
		t.Fatalf("goroutine 泄漏：%d → %d（Transport 未被复用）", base, after)
	}
}

// TestTransportCachedPerConfig 确认不同代理配置使用不同 Transport，
// 相同配置复用同一个。
func TestTransportCachedPerConfig(t *testing.T) {
	a, err := transportFor(&model.ProxySettings{Mode: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := transportFor(&model.ProxySettings{Mode: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("相同配置应复用同一 Transport")
	}

	c, err := transportFor(&model.ProxySettings{Mode: "custom", Type: "http", Host: "127.0.0.1", Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Errorf("不同配置不应共用 Transport")
	}

	CloseIdleConnections()
}

// TestCharsetFromContentType 覆盖 header 解析的各种写法。
func TestCharsetFromContentType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"text/xml; charset=GBK", "GBK"},
		{"text/xml;charset=gbk", "gbk"},
		{`text/xml; charset="GBK"`, "GBK"},
		{"text/xml; charset='GBK'", "GBK"},
		{"text/xml; charset=UTF-8; boundary=x", "UTF-8"},
		{"text/xml", ""},
		{"", ""},
		{"application/soap+xml; action=\"x\"; charset=GBK", "GBK"},
	}
	for _, c := range cases {
		if got := charsetFromContentType(c.in); got != c.want {
			t.Errorf("charsetFromContentType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
