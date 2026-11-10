package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"sync"

	"soapweb/internal/store"
)

// staticHandler serves the embedded frontend with a content-hash ETag and
// Cache-Control: no-cache. embed.FS has no modification time, so without them
// the browser would heuristically cache stale JS/CSS after an upgrade; no-cache
// means "store, but revalidate", a cheap 304 when unchanged.
func staticHandler(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	var (
		mu    sync.Mutex
		etags = map[string]string{}
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Defence in depth against content sniffing.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")

		if etag := etagFor(fs, r.URL.Path, &mu, etags); etag != "" {
			w.Header().Set("ETag", etag)
			if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// etagFor returns a strong ETag for the requested asset, or "" if it cannot be
// resolved (the FileServer will then produce its own 404).
func etagFor(fs http.FileSystem, urlPath string, mu *sync.Mutex, etags map[string]string) string {
	path := urlPath
	if path == "" || path == "/" {
		path = "/index.html"
	}

	mu.Lock()
	if tag, ok := etags[path]; ok {
		mu.Unlock()
		return tag
	}
	mu.Unlock()

	f, err := fs.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`

	mu.Lock()
	etags[path] = tag
	mu.Unlock()
	return tag
}

// etagMatches reports whether an If-None-Match header matches our ETag.
// It handles the comma-separated list and the weak "W/" prefix.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	// Every client-visible failure also lands in the log file: the browser only
	// shows the message, the file keeps it next to what led there.
	level := "警告"
	if status >= http.StatusInternalServerError {
		level = "错误"
	}
	log.Printf("[%s] %s", level, msg)
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON decodes a JSON request body into T. It writes a 400 response and
// returns ok=false when the body is invalid, so handlers stay one-liners.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var value T
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return value, false
	}
	return value, true
}

// isJSONRequest reports whether the request body is JSON.
func isJSONRequest(r *http.Request) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mediaType == "application/json"
}

// maxUploadBytes caps an uploaded project or WSDL document.
const maxUploadBytes = 32 << 20

// readUpload extracts the raw bytes from a multipart "file" field, or from the
// request body directly. The returned name is the uploaded filename if any.
func readUpload(r *http.Request) ([]byte, string, error) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			return nil, "", err
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, "", err
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		return data, header.Filename, err
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxUploadBytes))
	return data, "", err
}

// mapStoreErr converts store errors into appropriate HTTP responses.
func mapStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}
