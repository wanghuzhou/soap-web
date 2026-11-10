package api

import (
	"net/http"
	"strings"

	"soapweb/internal/model"
	"soapweb/internal/soapuiproj"
)

// registerProjectRoutes declares the nested project/service/operation/call CRUD
// surface. Wildcards are read with r.PathValue in each handler.
//
// Every handler resolves the request's workspace store with storeFrom(r); the
// project routes themselves are workspace-agnostic, which is what lets a
// workspace switch reuse the entire existing API unchanged.
func (s *Server) registerProjectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/projects", s.handleCreateProject)

	mux.HandleFunc("GET /api/projects/{pid}", s.handleGetProject)
	mux.HandleFunc("PUT /api/projects/{pid}", s.handleUpdateProject)
	mux.HandleFunc("DELETE /api/projects/{pid}", s.handleDeleteProject)
	mux.HandleFunc("GET /api/projects/{pid}/export", s.handleExportProject)
	// Narrow writes: avoid replacing the whole project tree for a one-field
	// change, which would clobber concurrent edits made in another tab.
	mux.HandleFunc("PUT /api/projects/{pid}/proxy", s.handleUpdateProjectProxy)
	mux.HandleFunc("PUT /api/projects/{pid}/name", s.handleRenameProject)

	mux.HandleFunc("POST /api/projects/{pid}/services", s.handleAddService)
	mux.HandleFunc("DELETE /api/projects/{pid}/services/{sid}", s.handleDeleteService)

	mux.HandleFunc("POST /api/projects/{pid}/services/{sid}/operations", s.handleAddOperation)
	mux.HandleFunc("DELETE /api/projects/{pid}/services/{sid}/operations/{oid}", s.handleDeleteOperation)

	mux.HandleFunc("POST /api/projects/{pid}/services/{sid}/operations/{oid}/calls", s.handleAddCall)
	mux.HandleFunc("PUT /api/projects/{pid}/services/{sid}/operations/{oid}/calls/{cid}", s.handleUpdateCall)
	mux.HandleFunc("DELETE /api/projects/{pid}/services/{sid}/operations/{oid}/calls/{cid}", s.handleDeleteCall)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, storeFrom(r).List())
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	project, ok := decodeJSON[model.Project](w, r)
	if !ok {
		return
	}
	if err := project.Proxy.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "代理配置无效: "+err.Error())
		return
	}
	saved, err := storeFrom(r).AddProject(project)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	project, err := storeFrom(r).Get(r.PathValue("pid"))
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	project, ok := decodeJSON[model.Project](w, r)
	if !ok {
		return
	}
	project.ID = r.PathValue("pid")
	if err := project.Proxy.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "代理配置无效: "+err.Error())
		return
	}
	if err := storeFrom(r).UpdateProject(project); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := storeFrom(r).DeleteProject(r.PathValue("pid")); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleUpdateProjectProxy updates only a project's proxy settings.
func (s *Server) handleUpdateProjectProxy(w http.ResponseWriter, r *http.Request) {
	proxy, ok := decodeJSON[*model.ProxySettings](w, r)
	if !ok {
		return
	}
	// Validate before touching the store so a bad config is a 400 (client
	// error), not a 500.
	if err := proxy.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "代理配置无效: "+err.Error())
		return
	}
	if err := storeFrom(r).UpdateProjectProxy(r.PathValue("pid"), proxy); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxy": proxy})
}

type renameProjectRequest struct {
	Name string `json:"name"`
}

// handleRenameProject updates only a project's name, so a rename cannot
// overwrite edits another tab made to the rest of the project.
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[renameProjectRequest](w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "工程名称不能为空")
		return
	}
	if err := storeFrom(r).RenameProject(r.PathValue("pid"), name); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("pid"), "name": name})
}

// handleExportProject serialises a project as SoapUI-compatible XML for download.
func (s *Server) handleExportProject(w http.ResponseWriter, r *http.Request) {
	project, err := storeFrom(r).Get(r.PathValue("pid"))
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	data, err := soapuiproj.Marshal(project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成 SoapUI XML 失败: "+err.Error())
		return
	}
	name := project.Name
	if name == "" {
		name = "project"
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(name)+`-soapui-project.xml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleAddService(w http.ResponseWriter, r *http.Request) {
	service, ok := decodeJSON[model.Service](w, r)
	if !ok {
		return
	}
	saved, err := storeFrom(r).AddService(r.PathValue("pid"), service)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	if err := storeFrom(r).DeleteService(r.PathValue("pid"), r.PathValue("sid")); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAddOperation(w http.ResponseWriter, r *http.Request) {
	operation, ok := decodeJSON[model.Operation](w, r)
	if !ok {
		return
	}
	saved, err := storeFrom(r).AddOperation(r.PathValue("pid"), r.PathValue("sid"), operation)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteOperation(w http.ResponseWriter, r *http.Request) {
	if err := storeFrom(r).DeleteOperation(r.PathValue("pid"), r.PathValue("sid"), r.PathValue("oid")); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAddCall(w http.ResponseWriter, r *http.Request) {
	call, ok := decodeJSON[model.Call](w, r)
	if !ok {
		return
	}
	saved, err := storeFrom(r).AddCall(r.PathValue("pid"), r.PathValue("sid"), r.PathValue("oid"), call)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleUpdateCall(w http.ResponseWriter, r *http.Request) {
	call, ok := decodeJSON[model.Call](w, r)
	if !ok {
		return
	}
	call.ID = r.PathValue("cid")
	if err := storeFrom(r).UpdateCall(r.PathValue("pid"), r.PathValue("sid"), r.PathValue("oid"), call); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, call)
}

func (s *Server) handleDeleteCall(w http.ResponseWriter, r *http.Request) {
	if err := storeFrom(r).DeleteCall(r.PathValue("pid"), r.PathValue("sid"), r.PathValue("oid"), r.PathValue("cid")); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// sanitizeFilename keeps filename-safe characters only.
func sanitizeFilename(s string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", `"`, "_",
		"<", "_", ">", "_", "|", "_", " ", "_",
	)
	return replacer.Replace(s)
}
