package applog

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// restoreLogger puts the default logger back after a test replaced its output.
func restoreLogger(t *testing.T) {
	t.Helper()
	Close()
	log.SetOutput(os.Stderr)
}

// TestInitWritesConsoleAndFile checks the two destinations receive the same
// message, the file with a timestamp and the console truncated.
func TestInitWritesConsoleAndFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	if filepath.Dir(path) != dir || filepath.Base(path) != FileName {
		t.Errorf("log path = %q, want %s", path, filepath.Join(dir, FileName))
	}

	log.Printf("解析 WSDL 失败: %s", strings.Repeat("很长的错误信息", 40))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "soap-web 启动") {
		t.Errorf("log file is missing the startup banner:\n%s", text)
	}
	if !strings.Contains(text, "解析 WSDL 失败") {
		t.Errorf("log file is missing the error line:\n%s", text)
	}
	// The file keeps the whole message, unlike the console.
	if !strings.Contains(text, "很长的错误信息很长的错误信息") {
		t.Errorf("long message was truncated in the file")
	}
	if !strings.Contains(text, time.Now().Format("2006-01-02")) {
		t.Errorf("log line has no timestamp:\n%s", text)
	}
}

// TestDebugfSkipsConsole pins the level boundary: a debug line is written to
// the file but never echoed to the console.
func TestDebugfSkipsConsole(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	// Swap stderr for a pipe so the console output can be inspected.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	Debugf("[请求失败 400] 缺少 url")
	log.Printf("[错误] 保存失败")
	_ = w.Close()
	os.Stderr = origStderr

	console, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read console: %v", err)
	}
	if strings.Contains(string(console), "缺少 url") {
		t.Errorf("debug line reached the console: %q", string(console))
	}
	if !strings.Contains(string(console), "[错误] 保存失败") {
		t.Errorf("error line missing from the console: %q", string(console))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "[调试] [请求失败 400] 缺少 url") {
		t.Errorf("debug line missing from the log file:\n%s", string(data))
	}
}

// TestInitRotatesLargeFile keeps the log directory bounded when it grows past
// the cap.
func TestInitRotatesLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	big := strings.Repeat("x", maxSize+1)
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= maxSize {
		t.Errorf("active log is still %d bytes; rotation did not happen", info.Size())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var backups int
	for _, e := range entries {
		if e.Name() != FileName {
			backups++
		}
	}
	if backups != 1 {
		t.Errorf("found %d rotated files, want 1", backups)
	}
}

// TestInitReportsUnusableDirectory keeps the caller able to start without a
// log file instead of panicking.
func TestInitReportsUnusableDirectory(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, DirName)
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(blocker); err == nil {
		t.Fatal("Init should fail when the log directory is a file")
	}
}

func TestTrimNewline(t *testing.T) {
	for in, want := range map[string]string{"a\n": "a", "a\r\n": "a", "a": "a", "\n\n": ""} {
		if got := trimNewline(in); got != want {
			t.Errorf("trimNewline(%q) = %q, want %q", in, got, want)
		}
	}
}
