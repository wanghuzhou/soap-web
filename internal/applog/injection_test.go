package applog

import (
	"log"
	"os"
	"strings"
	"testing"
)

// logLines returns the file's physical lines, without the trailing empty one.
func logLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// isRecordStart reports whether a line begins a log record: it must carry the
// "2006-01-02 15:04:05.000 " timestamp this package writes.
func isRecordStart(line string) bool {
	const layout = "2006-01-02 15:04:05.000"
	return len(line) > len(layout) &&
		line[4] == '-' && line[7] == '-' && line[10] == ' ' &&
		line[13] == ':' && line[16] == ':' && line[19] == '.' &&
		line[len(layout)] == ' '
}

// TestNewlinesCannotForgeLogRecords is the regression test for log-line
// injection. Messages embed user text (workspace and project names, URLs,
// upstream error bodies); an embedded newline used to start a line with no
// timestamp of its own, so "[错误] 伪造的日志行" was indistinguishable from a
// real entry when reading the file afterwards.
func TestNewlinesCannotForgeLogRecords(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	log.Printf("已创建工作空间：evil\n[错误] 伪造的日志行\n[调试] 也是伪造的")

	lines := logLines(t, path)
	var forged []string
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		if !isRecordStart(ln) && !strings.Contains(ln, continuationPrefix) {
			forged = append(forged, ln)
		}
	}
	if len(forged) > 0 {
		t.Fatalf("%d line(s) can be mistaken for a log record:\n%q", len(forged), forged)
	}

	// The continuation must still carry the original text, so nothing is lost.
	text := strings.Join(lines, "\n")
	for _, want := range []string{"已创建工作空间：evil", "伪造的日志行", "也是伪造的"} {
		if !strings.Contains(text, want) {
			t.Errorf("log lost %q", want)
		}
	}
}

// TestMultilineMessageStaysOneRecord checks the structure: one timestamp, then
// indented continuations, so a record greps as a unit.
func TestMultilineMessageStaysOneRecord(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	// The startup banner is written by Init, so count from a known offset.
	before := len(logLines(t, path))
	log.Printf("第一行\n第二行\n第三行")
	lines := logLines(t, path)[before:]

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), lines)
	}
	if !isRecordStart(lines[0]) {
		t.Errorf("first line is not a record start: %q", lines[0])
	}
	for i, ln := range lines[1:] {
		if isRecordStart(ln) {
			t.Errorf("continuation %d looks like a new record: %q", i+1, ln)
		}
		if !strings.Contains(ln, continuationPrefix) {
			t.Errorf("continuation %d is not marked: %q", i+1, ln)
		}
	}
}

// TestWindowsNewlinesAreFoldedToo covers \r\n, which a Windows-sourced error
// message can carry.
func TestWindowsNewlinesAreFoldedToo(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	log.Printf("甲\r\n乙")
	lines := logLines(t, path)
	last := lines[len(lines)-1]
	if strings.Contains(last, "\r") {
		t.Errorf("carriage return survived into the log: %q", last)
	}
	if !strings.Contains(last, continuationPrefix+"乙") {
		t.Errorf("continuation not marked as expected: %q", last)
	}
}

// TestSingleLineMessageUnchanged makes sure the folding did not alter the
// common case.
func TestSingleLineMessageUnchanged(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { restoreLogger(t) })

	log.Printf("普通的一行")
	lines := logLines(t, path)
	last := lines[len(lines)-1]
	if !isRecordStart(last) {
		t.Fatalf("not a record start: %q", last)
	}
	if !strings.HasSuffix(last, " 普通的一行") {
		t.Errorf("message altered: %q", last)
	}
	if strings.Contains(last, continuationPrefix) {
		t.Errorf("single-line message got a continuation marker: %q", last)
	}
}
