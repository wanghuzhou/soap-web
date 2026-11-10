package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureLog redirects the standard logger into buf for the duration of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// TestLoggingRecordsFailures pins the behaviour the log file relies on: a
// client error is recorded at debug level (file only), and a routine success
// is not recorded at all.
func TestLoggingRecordsFailures(t *testing.T) {
	_, mux, _ := newTestServer(t)
	buf := captureLog(t)

	// A missing project -> 404 through mapStoreErr -> writeErr.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	out := buf.String()
	if !strings.Contains(out, "[调试] [请求失败 404]") {
		t.Errorf("client error was not logged at debug level:\n%s", out)
	}
	if !strings.Contains(out, "[调试] GET /api/projects/nope -> 404") {
		t.Errorf("access line missing:\n%s", out)
	}
	// A 4xx must not be dressed up as a warning: the console stays quiet.
	if strings.Contains(out, "[警告]") {
		t.Errorf("client error should not be a warning:\n%s", out)
	}

	buf.Reset()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(buf.String(), "GET /api/projects") {
		t.Errorf("successful read should not be logged:\n%s", buf.String())
	}
}

// TestStaleWorkspaceIsDebugLevel covers the 409 path: it is expected whenever a
// window is left open across a switch, so it must not reach the console.
func TestStaleWorkspaceIsDebugLevel(t *testing.T) {
	_, mux, _ := newTestServer(t)
	buf := captureLog(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set(workspaceHeader, "stale-workspace")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}

	out := buf.String()
	if !strings.Contains(out, "[调试] 拒绝过期标签页的请求") {
		t.Errorf("stale-tab rejection was not logged:\n%s", out)
	}
	if strings.Contains(out, "[警告]") {
		t.Errorf("stale-tab rejection should not be a warning:\n%s", out)
	}
}

// TestLoggingRecordsServerErrors marks 5xx as 错误 rather than 警告.
func TestLoggingRecordsServerErrors(t *testing.T) {
	buf := captureLog(t)

	h := withLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusInternalServerError, "保存失败: 磁盘已满")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/projects", nil))

	out := buf.String()
	if !strings.Contains(out, "[错误] 保存失败: 磁盘已满") {
		t.Errorf("server error message not logged:\n%s", out)
	}
	if !strings.Contains(out, "POST /api/projects -> 500") {
		t.Errorf("access line missing:\n%s", out)
	}
}

// TestRecorderDefaultsToOK covers handlers that write without WriteHeader.
func TestRecorderDefaultsToOK(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, err := rec.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.status)
	}
}
