// Package api wires HTTP handlers for the frontend. It exposes a small JSON
// REST surface under /api and serves the embedded SPA for everything else.
package api

import (
	"context"
	"log"
	"net/http"
	"strings"

	"soapweb/internal/model"
	"soapweb/internal/soapuiproj"
	"soapweb/internal/store"
	"soapweb/internal/transport"
	"soapweb/internal/workspace"
)

// Server holds shared dependencies for handlers.
type Server struct {
	// StaticFS is the embedded frontend filesystem (rooted at web/).
	StaticFS http.FileSystem
	// Workspaces owns settings.json and the active project store.
	Workspaces *workspace.Registry
}

// Register attaches all routes to mux. Routes use Go's method + wildcard
// patterns so each handler owns exactly one operation and path parameters are
// read with r.PathValue.
//
// API handlers run behind withWorkspace, which pins each request to the
// workspace that was active when it started and rejects writes from a tab that
// still points at a workspace the server has since switched away from.
func (s *Server) Register(mux *http.ServeMux) {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/health", s.handleHealth)
	apiMux.HandleFunc("POST /api/send", s.handleSend)
	apiMux.HandleFunc("POST /api/import/soapui", s.handleImportSoapUI)
	apiMux.HandleFunc("POST /api/wsdl/parse", s.handleParseWSDL)
	apiMux.HandleFunc("POST /api/wsdl/fetch", s.handleFetchWSDL)

	s.registerProjectRoutes(apiMux)
	s.registerWorkspaceRoutes(apiMux)

	mux.Handle("/api/", withLogging(s.withWorkspace(apiMux)))
	mux.Handle("/", staticHandler(s.StaticFS))
}

// workspaceHeader identifies the workspace a request was issued against. The
// frontend sends it with every API call; a mismatch means another tab switched
// workspaces in the meantime.
const workspaceHeader = "X-Workspace-Id"

// requestWorkspace is the store/info pair a single request operates on. It is
// captured once, at the start of the request, so a long WSDL import keeps
// writing to the workspace it started in even if the user switches meanwhile.
type requestWorkspace struct {
	store *store.Store
	info  workspace.Info
}

type requestWorkspaceKey struct{}

// withWorkspace pins the request to the active workspace.
func (s *Server) withWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st, info := s.Workspaces.Active()

		// The workspace endpoints themselves, and health, must stay reachable
		// from a stale tab: health is how it discovers the switch, and the
		// workspace API is how it recovers.
		if !workspaceExempt(r) {
			if want := r.Header.Get(workspaceHeader); want != "" && want != info.ID {
				log.Printf("拒绝过期标签页的请求：%s %s（标签页工作空间 %s，当前 %s）",
					r.Method, r.URL.Path, want, info.ID)
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":     "工作空间已在其他窗口切换，请先同步后再操作",
					"code":      "workspace_mismatch",
					"workspace": s.Workspaces.CurrentInfo(),
				})
				return
			}
		}

		w.Header().Set(workspaceHeader, info.ID)
		ctx := context.WithValue(r.Context(), requestWorkspaceKey{}, requestWorkspace{store: st, info: info})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// workspaceExempt reports whether a request bypasses the stale-workspace check.
// Only the workspace-management endpoints are exempt; import/export are scoped
// to the active workspace and must be rejected from a stale tab.
func workspaceExempt(r *http.Request) bool {
	switch {
	case r.URL.Path == "/api/health",
		r.URL.Path == "/api/workspaces",
		r.URL.Path == "/api/workspaces/switch":
		return true
	case strings.HasPrefix(r.URL.Path, "/api/workspaces/") &&
		(r.Method == http.MethodPatch || r.Method == http.MethodDelete):
		return true
	}
	return false
}

// workspaceFrom returns the workspace captured for this request. Every API
// handler is mounted behind withWorkspace, so a missing value is a programming
// error rather than a client error.
func workspaceFrom(r *http.Request) requestWorkspace {
	rw, _ := r.Context().Value(requestWorkspaceKey{}).(requestWorkspace)
	if rw.store == nil {
		panic("api: handler reached without workspace context")
	}
	return rw
}

// storeFrom is the common shorthand for the request's project store.
func storeFrom(r *http.Request) *store.Store {
	return workspaceFrom(r).store
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// The workspace rides along so the frontend's 10-second heartbeat notices a
	// switch made in another tab without polling the workspace list.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"workspace": s.Workspaces.CurrentInfo(),
	})
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
		if p, err := storeFrom(r).Get(req.ProjectID); err == nil {
			proxyCfg = p.Proxy
		}
	}
	resp := transport.Send(req, proxyCfg)
	if resp.Error != "" {
		// Returned as 200 with an error field, so this log line is the only
		// place a failed call is recorded.
		log.Printf("[错误] 发送请求失败：%s %s：%s", req.Method, req.URL, resp.Error)
	}
	writeJSON(w, http.StatusOK, resp)
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
	saved, err := storeFrom(r).AddProject(*project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存工程失败: "+err.Error())
		return
	}
	// workspaceId tells the client where a long-running import landed when the
	// workspace was switched while the upload was in flight.
	writeJSON(w, http.StatusOK, map[string]any{
		"project":     saved,
		"workspaceId": workspaceFrom(r).info.ID,
	})
}
