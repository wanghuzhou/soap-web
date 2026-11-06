// Package api wires HTTP handlers for the frontend. It exposes a small JSON
// REST surface under /api and serves the embedded SPA for everything else.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"

	"soapweb/internal/model"
	"soapweb/internal/soapuiproj"
	"soapweb/internal/store"
	"soapweb/internal/transport"
	"soapweb/internal/wsdl"
	"soapweb/internal/wsdlfetch"
)

// Server holds shared dependencies for handlers.
type Server struct {
	// StaticFS is the embedded frontend filesystem (rooted at web/).
	StaticFS http.FileSystem
	// Store persists imported and user-created projects.
	Store *store.Store
}

// Register attaches all routes to mux. Routes use Go's method + wildcard
// patterns so each handler owns exactly one operation and path parameters are
// read with r.PathValue.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/send", s.handleSend)
	mux.HandleFunc("POST /api/import/soapui", s.handleImportSoapUI)
	mux.HandleFunc("POST /api/wsdl/parse", s.handleParseWSDL)
	mux.HandleFunc("POST /api/wsdl/fetch", s.handleFetchWSDL)

	s.registerProjectRoutes(mux)

	mux.Handle("GET /", staticHandler(s.StaticFS))
}

// staticHandler serves the embedded frontend with explicit cache headers.
//
// http.FileServer over an embed.FS emits neither Last-Modified nor ETag, because
// embedded files have a zero modification time. With no Cache-Control either,
// browsers fall back to heuristic caching: after upgrading the binary, the
// browser could keep serving the old JS/CSS, producing the confusing "backend
// is fixed but the UI still misbehaves" situation. ES modules make it worse,
// since a stale entry caches the whole dependency graph.
//
// The fix is a strong ETag derived from the file content plus
// `Cache-Control: no-cache`, which means "store it, but revalidate before use".
// The browser therefore always confirms freshness (a cheap 304 when unchanged)
// and can never run stale assets. Content hashes are computed once per file and
// cached for the lifetime of the process, so revalidation costs a map lookup.
func staticHandler(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	var (
		mu     sync.Mutex
		etags  = map[string]string{}
		hashes = map[string]string{}
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Defence in depth against content sniffing.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")

		if etag := etagFor(fs, r.URL.Path, &mu, etags, hashes); etag != "" {
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
func etagFor(fs http.FileSystem, urlPath string, mu *sync.Mutex, etags, hashes map[string]string) string {
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
	hashes[path] = tag
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

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSend executes a raw HTTP/SOAP call.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[model.SendRequest](w, r)
	if !ok {
		return
	}
	if req.URL == "" {
		writeErr(w, http.StatusBadRequest, "缺少 url")
		return
	}
	// Resolve the project's proxy settings (if the request belongs to a project).
	var proxyCfg *model.ProxySettings
	if req.ProjectID != "" {
		if p, err := s.Store.Get(req.ProjectID); err == nil {
			proxyCfg = p.Proxy
		}
	}
	writeJSON(w, http.StatusOK, transport.Send(req, proxyCfg))
}

// handleImportSoapUI parses an uploaded SoapUI project and persists it.
func (s *Server) handleImportSoapUI(w http.ResponseWriter, r *http.Request) {
	data, name, err := readUpload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	project, err := soapuiproj.Parse(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "解析 SoapUI 工程失败: "+err.Error())
		return
	}
	if project.Name == "" {
		project.Name = name
	}
	saved, err := s.Store.AddProject(*project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存工程失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// handleParseWSDL parses a WSDL and persists it as a new project containing a
// single service.
//
// Despite its name it accepts both import sources: a plain/multipart upload
// (readUpload) and a JSON body {"url": "..."} that asks the backend to fetch
// the document. Keeping one endpoint means the file and URL paths cannot drift
// apart; POST /api/wsdl/fetch is an explicit alias for the URL case.
func (s *Server) handleParseWSDL(w http.ResponseWriter, r *http.Request) {
	if isJSONRequest(r) {
		s.handleFetchWSDL(w, r)
		return
	}
	data, name, err := readUpload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	service, err := wsdl.Parse(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "解析 WSDL 失败: "+err.Error())
		return
	}
	saved, err := s.persistWSDLProject(service, name, name, "wsdl")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存 WSDL 工程失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": service,
		"project": saved,
	})
}

// fetchWSDLRequest is the payload for POST /api/wsdl/fetch.
type fetchWSDLRequest struct {
	URL string `json:"url"`
	// ProjectID is optional. When set, that project's proxy configuration is
	// used for the download, which lets an intranet WSDL be reached through the
	// same proxy as the services the project debugs.
	ProjectID string `json:"projectId"`
}

// handleFetchWSDL downloads a WSDL from a URL, parses it and persists it as a
// new project — the remote counterpart of handleParseWSDL.
func (s *Server) handleFetchWSDL(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[fetchWSDLRequest](w, r)
	if !ok {
		return
	}

	var proxyCfg *model.ProxySettings
	if req.ProjectID != "" {
		if p, err := s.Store.Get(req.ProjectID); err == nil {
			proxyCfg = p.Proxy
		}
	}

	fetched, err := wsdlfetch.Fetch(req.URL, proxyCfg)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, wsdlfetch.ErrInvalidURL):
			status = http.StatusBadRequest
		case errors.Is(err, wsdlfetch.ErrNotWSDL):
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}

	service, err := wsdl.Parse(fetched.Data)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "解析 WSDL 失败: "+err.Error())
		return
	}
	project, err := s.persistWSDLProject(service, fetched.Name, fetched.FinalURL, "wsdl-url")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存 WSDL 工程失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"service": service,
		"project": project,
		"url":     fetched.FinalURL,
	})
}

// persistWSDLProject turns a parsed WSDL service into a saved project.
//
// source records where the document came from (a filename for uploads, the
// final URL for remote imports) so the project tree can show its origin.
//
// Every operation gets one generated request sample (a model.Call). SoapUI
// creates a request per operation on WSDL import, and without it opening a
// freshly imported project shows an empty editor until the user manually adds
// one — which looks exactly like "the import failed" to a first-time user.
func (s *Server) persistWSDLProject(service *wsdl.Result, fallbackName, source, projectSource string) (model.Project, error) {
	serviceName := service.Name
	if serviceName == "" {
		serviceName = fallbackName
	}
	// For WSDL-parsed operations the generated Request IS the pure structural
	// skeleton (no business data), so mirror it into Template as well.
	for i := range service.Operations {
		op := &service.Operations[i]
		if op.Template == "" {
			op.Template = op.Request
		}
		if len(op.Calls) == 0 {
			body := op.Template
			if body == "" {
				body = op.Request
			}
			if body != "" {
				title := op.Name
				if title == "" {
					title = "请求"
				}
				op.Calls = []model.Call{{
					Title:    title,
					Endpoint: op.Endpoint,
					Action:   op.SoapAction,
					Request:  body,
				}}
			}
		}
	}
	project := model.Project{
		Name:   serviceName,
		Source: projectSource,
		Services: []model.Service{{
			Name:       serviceName,
			Wsdl:       source,
			Operations: service.Operations,
		}},
	}
	if project.Name == "" {
		project.Name = "WSDL 工程"
	}
	return s.Store.AddProject(project)
}

// isJSONRequest reports whether the request body is JSON.
func isJSONRequest(r *http.Request) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mediaType == "application/json"
}

// readUpload extracts the raw bytes from a multipart "file" field, or from the
// request body directly. The returned name is the uploaded filename if any.
func readUpload(r *http.Request) ([]byte, string, error) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
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
	data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
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
