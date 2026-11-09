// Package applog sends the standard logger to the console and to a rotating
// file in a logs/ directory next to the data directory.
//
// The console is what a user sees while the window is open; the file is what
// survives it — a startup failure, a crash, or a "why did it say that" report
// days later. Both are formatted here because the console should stay short
// while the file keeps the detail needed to diagnose a problem.
package applog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DirName is the log directory created next to the data directory.
	DirName = "logs"
	// FileName is the active log file inside DirName.
	FileName = "soap-web.log"
	// maxSize rotates the file once it grows past this many bytes; network
	// failures on a long-running instance can add up quickly.
	maxSize = 4 << 20
	// consoleMaxRunes keeps console lines readable; the file keeps everything.
	consoleMaxRunes = 150
)

// current is the active sink, if any, so Close can release the file. Tests on
// Windows cannot delete a directory that still holds an open log file.
var current atomic.Pointer[sink]

// Close stops writing to the log file and returns the standard logger to the
// console. Safe to call when Init never ran or already failed.
func Close() {
	if s := current.Swap(nil); s != nil {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
		s.mu.Lock()
		_ = s.file.Close()
		s.mu.Unlock()
	}
}

// Init points the standard logger at the console and at <dir>/soap-web.log.
// It returns the file path; on failure the console logger is left untouched so
// the caller can report the error and carry on.
func Init(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建日志目录 %s 失败: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("打开日志文件 %s 失败: %w", path, err)
	}
	if info, statErr := f.Stat(); statErr == nil && info.Size() >= maxSize {
		_ = f.Close()
		rotate(path)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return "", fmt.Errorf("重建日志文件 %s 失败: %w", path, err)
		}
	}

	s := &sink{file: f, path: path}
	// The sink timestamps both destinations itself; the logger's own prefix
	// would only duplicate it.
	log.SetFlags(0)
	log.SetOutput(s)
	current.Store(s)
	// A separator makes restarts and "what happened before it died" obvious.
	log.Printf("================ soap-web 启动 ================")
	return path, nil
}

// rotate renames the current log to soap-web-<时间>.log, replacing an older file
// with the same name. Failures are ignored: losing history beats refusing to
// log.
func rotate(path string) {
	ext := filepath.Ext(path)
	backup := path[:len(path)-len(ext)] + "-" + time.Now().Format("20060102-150405") + ext
	_ = os.Remove(backup)
	_ = os.Rename(path, backup)
}

// sink formats each record twice: a short line for the console and a
// timestamped line for the file.
type sink struct {
	mu   sync.Mutex
	file *os.File
	path string
	now  func() time.Time
}

func (s *sink) Write(p []byte) (int, error) {
	msg := trimNewline(string(p))

	short := []rune(msg)
	if len(short) > consoleMaxRunes {
		short = append(short[:consoleMaxRunes], '…')
	}
	_, _ = fmt.Fprintln(os.Stderr, string(short))

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now == nil {
		s.now = time.Now
	}
	line := s.now().Format("2006-01-02 15:04:05.000") + " " + msg + "\n"
	if _, err := s.file.WriteString(line); err != nil {
		// A full disk or a removed file must not kill the app.
		_, _ = fmt.Fprintf(os.Stderr, "写入日志文件 %s 失败: %v\n", s.path, err)
	}
	return len(p), nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
