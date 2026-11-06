// Package api wires HTTP handlers for the frontend. It exposes a small JSON
// REST surface under /api and serves the embedded SPA for everything else.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"soapweb/internal/model"
	"soapweb/internal/soapuiproj"
	"soapweb/internal/store"
	"soapweb/internal/transport"
	"soapweb/internal/wsdl"
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

	s.registerProjectRoutes(mux)

	mux.Handle("GET /", http.FileServer(s.StaticFS))
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
func (s *Server) handleParseWSDL(w http.ResponseWriter, r *http.Request) {
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
	serviceName := service.Name
	if serviceName == "" {
		serviceName = name
	}
	// For WSDL-parsed operations the generated Request IS the pure structural
	// skeleton (no business data), so mirror it into Template as well.
	for i := range service.Operations {
		if service.Operations[i].Template == "" {
			service.Operations[i].Template = service.Operations[i].Request
		}
	}
	project := model.Project{
		Name:   serviceName,
		Source: "wsdl",
		Services: []model.Service{{
			Name:       serviceName,
			Wsdl:       name,
			Operations: service.Operations,
		}},
	}
	if project.Name == "" {
		project.Name = "WSDL 工程"
	}
	saved, err := s.Store.AddProject(project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存 WSDL 工程失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": service,
		"project": saved,
	})
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
