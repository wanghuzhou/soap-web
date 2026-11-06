package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testFS builds an in-memory asset filesystem for the static handler.
func testFS() http.FileSystem {
	return http.FS(fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><html><body>hi</body></html>")},
		"style.css":  {Data: []byte("body{color:#000}")},
		"js/app.js":  {Data: []byte("export const x = 1;")},
	})
}

// TestStaticHandlerSetsCacheHeaders 回归：embed.FS 的文件 ModTime 为零，
// http.FileServer 既不输出 Last-Modified 也不输出 ETag，浏览器会走启发式
// 缓存 —— 升级二进制后可能一直用旧的 JS/CSS。
func TestStaticHandlerSetsCacheHeaders(t *testing.T) {
	h := staticHandler(testFS())

	for _, path := range []string{"/", "/style.css", "/js/app.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d", path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, got)
		}
		if got := w.Header().Get("ETag"); got == "" {
			t.Errorf("%s: 缺少 ETag", path)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", path, got)
		}
	}

	// http.FileServer redirects /index.html to / by design; that redirect
	// should still carry the safety headers.
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("/index.html: status = %d, want 301", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("/index.html: Cache-Control = %q", got)
	}
}

// TestStaticHandlerRevalidation 内容未变时应返回 304。
func TestStaticHandlerRevalidation(t *testing.T) {
	h := staticHandler(testFS())

	req := httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("首次请求应返回 ETag")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", w2.Code)
	}
	if body := w2.Body.String(); body != "" {
		t.Errorf("304 不应有响应体，实际 %q", body)
	}
}

// TestStaticHandlerETagDiffersPerFile 不同内容的 ETag 必须不同。
func TestStaticHandlerETagDiffersPerFile(t *testing.T) {
	h := staticHandler(testFS())
	tags := map[string]string{}
	for _, path := range []string{"/index.html", "/style.css", "/js/app.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		tag := w.Header().Get("ETag")
		if prev, ok := tags[tag]; ok {
			t.Errorf("%s 与 %s 的 ETag 相同 (%s)", path, prev, tag)
		}
		tags[tag] = path
	}
}

// TestStaticHandlerMissingFile 不存在的文件由 FileServer 返回 404。
func TestStaticHandlerMissingFile(t *testing.T) {
	h := staticHandler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/nope.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestETagMatches 覆盖 If-None-Match 的解析：逗号列表、弱前缀、通配。
func TestETagMatches(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		header string
		want   bool
	}{
		{`"abc123"`, true},
		{`"other"`, false},
		{`"other", "abc123"`, true},
		{`W/"abc123"`, true},
		{`*`, true},
		{``, false},
		{` "abc123" `, true},
	}
	for _, c := range cases {
		if got := etagMatches(c.header, etag); got != c.want {
			t.Errorf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

// TestStaticHandlerServesContent 确认内容真的被送出（不只是头正确）。
func TestStaticHandlerServesContent(t *testing.T) {
	h := staticHandler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "color") {
		t.Fatalf("body = %q", w.Body.String())
	}
}
