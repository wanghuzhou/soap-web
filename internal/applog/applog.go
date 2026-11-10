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
	"strings"
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
	// debugPrefix marks a line as file-only. Routine client errors (a stale
	// tab's 409, a validation failure) are useful when reading the log later
	// but would flood the console the user is watching.
	debugPrefix = "[调试] "
)

// Debugf writes a file-only log line. The console stays for warnings and
// errors, so a user cannot tell a validation failure from a broken page.
func Debugf(format string, args ...any) {
	log.Printf(debugPrefix+format, args...)
}

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
	size := int64(0)
	if info, statErr := f.Stat(); statErr == nil {
		size = info.Size()
	}
	if size >= maxSize {
		_ = f.Close()
		_ = rotate(path)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return "", fmt.Errorf("重建日志文件 %s 失败: %w", path, err)
		}
		size = 0
	}

	s := &sink{file: f, path: path, size: size}
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
// with the same name. A failure is returned so the caller can decide whether to
// keep appending to the current file; losing history beats refusing to log.
func rotate(path string) error {
	ext := filepath.Ext(path)
	backup := path[:len(path)-len(ext)] + "-" + time.Now().Format("20060102-150405") + ext
	_ = os.Remove(backup)
	return os.Rename(path, backup)
}

// sink formats each record twice: a short line for the console and a
// timestamped line for the file.
type sink struct {
	mu   sync.Mutex
	file *os.File
	path string
	now  func() time.Time
	// size is how much has been written to file, so the file is rotated while
	// the process is running and not only at the next startup.
	size int64
}

func (s *sink) Write(p []byte) (int, error) {
	msg := trimNewline(string(p))

	// A debug line belongs in the file only; echoing it would defeat the
	// point of keeping the console quiet during routine failures.
	if !strings.HasPrefix(msg, debugPrefix) {
		short := []rune(msg)
		if len(short) > consoleMaxRunes {
			short = append(short[:consoleMaxRunes], '…')
		}
		_, _ = fmt.Fprintln(os.Stderr, string(short))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now == nil {
		s.now = time.Now
	}
	if s.size >= maxSize {
		s.rotateLocked()
	}
	if s.file == nil {
		// Rotation or a previous write failed. Try once more to reopen; if the
		// directory is gone, keep logging to the console rather than panicking.
		s.openLocked()
	}
	if s.file == nil {
		return len(p), nil
	}
	line := s.now().Format("2006-01-02 15:04:05.000") + " " + msg + "\n"
	n, err := s.file.WriteString(line)
	s.size += int64(n)
	if err != nil {
		// A full disk or a removed file must not kill the app.
		_, _ = fmt.Fprintf(os.Stderr, "写入日志文件 %s 失败: %v\n", s.path, err)
	}
	return len(p), nil
}

// rotateLocked closes the active file, renames it aside, and opens a fresh one.
// The caller holds s.mu.
func (s *sink) rotateLocked() {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	if err := rotate(s.path); err != nil {
		// Renaming failed (for example the file is held open by another
		// process on Windows). Keep appending to it.
		_, _ = fmt.Fprintf(os.Stderr, "轮转日志文件 %s 失败: %v\n", s.path, err)
	}
	// Reset the counter whether or not the rename succeeded. A failed rename
	// leaves the old file in place; retrying on the very next line would flood
	// the console, so wait for another maxSize worth of lines instead.
	s.size = 0
	s.openLocked()
}

// openLocked opens the log file for append. The caller holds s.mu and owns
// s.size: opening must not silently restore the old size, or a failed rotation
// would retry on every line.
func (s *sink) openLocked() {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "重建日志文件 %s 失败: %v\n", s.path, err)
		return
	}
	s.file = f
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
