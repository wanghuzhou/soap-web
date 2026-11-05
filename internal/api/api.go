// Package api wires HTTP handlers for the frontend. It exposes a small JSON
// REST surface under /api and serves the embedded SPA for everything else.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"soapweb/internal/model"
	"soapweb/internal/soapuiproj"
	"soapweb/internal/transport"
	"soapweb/internal/wsdl"
)

// Server holds shared dependencies for handlers.
type Server struct {
	// StaticFS is the embedded frontend filesystem (rooted at web/).
	StaticFS http.FileSystem
}

// Register attaches all routes to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/send", s.handleSend)
	mux.HandleFunc("/api/import/soapui", s.handleImportSoapUI)
	mux.HandleFunc("/api/wsdl/parse", s.handleParseWSDL)
	mux.Handle("/", http.FileServer(s.StaticFS))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSend executes a raw HTTP/SOAP call.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req model.SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	if req.URL == "" {
		writeErr(w, http.StatusBadRequest, "缺少 url")
		return
	}
	writeJSON(w, http.StatusOK, transport.Send(req))
}

// handleImportSoapUI accepts an uploaded SoapUI project xml (multipart field
// "file" or raw body) and returns the parsed project model.
func (s *Server) handleImportSoapUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	data, name, err := readUpload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	proj, err := soapuiproj.Parse(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "解析 SoapUI 工程失败: "+err.Error())
		return
	}
	if proj.Name == "" {
		proj.Name = name
	}
	writeJSON(w, http.StatusOK, proj)
}

// handleParseWSDL accepts a WSDL (upload field "file", or a "url"/"text" form
// value) and returns the parsed service with generated request templates.
func (s *Server) handleParseWSDL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	data, _, err := readUpload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	svc, err := wsdl.Parse(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "解析 WSDL 失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

// readUpload extracts the raw bytes from a multipart "file" field, or from the
// request body directly. The returned name is the uploaded filename if any.
func readUpload(r *http.Request) ([]byte, string, error) {
	ct := r.Header.Get("Content-Type")
	if len(ct) >= 19 && ct[:19] == "multipart/form-data" {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, "", err
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		return b, hdr.Filename, err
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	return b, "", err
}

// LoadFile is a helper for the CLI: read a project/ wsdl from disk.
func LoadFile(path string) ([]byte, error) { return os.ReadFile(path) }
