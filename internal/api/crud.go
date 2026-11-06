package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"soapweb/internal/model"
	"soapweb/internal/soapuiproj"
)

// handleProjects handles /api/projects (list + create).
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Store.List())
	case http.MethodPost:
		var p model.Project
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
			return
		}
		saved, err := s.Store.AddProject(p)
		if err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// handleProjectItem routes all nested CRUD under /api/projects/{pid}/...
//
// Patterns:
//
//	/projects/{pid}                                  GET/PUT/DELETE
//	/projects/{pid}/services                         POST
//	/projects/{pid}/services/{sid}                   DELETE
//	/projects/{pid}/services/{sid}/operations        POST
//	/projects/{pid}/services/{sid}/operations/{oid}  DELETE
//	/projects/{pid}/services/{sid}/operations/{oid}/calls        POST
//	/projects/{pid}/services/{sid}/operations/{oid}/calls/{cid}  PUT/DELETE
func (s *Server) handleProjectItem(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path) // api, projects, pid, ...
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "projects" {
		writeErr(w, http.StatusBadRequest, "缺少工程 ID")
		return
	}
	pid := parts[2]
	rest := parts[3:]

	// /projects/{pid}
	if len(rest) == 0 {
		s.projectRoot(w, r, pid)
		return
	}

	switch rest[0] {
	case "services":
		s.servicesRoute(w, r, pid, rest[1:])
	case "export":
		s.exportProject(w, r, pid)
	default:
		writeErr(w, http.StatusNotFound, "未知路径")
	}
}

// exportProject serialises a project as SoapUI-compatible XML for download.
func (s *Server) exportProject(w http.ResponseWriter, r *http.Request, pid string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	p, err := s.Store.Get(pid)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	data, err := soapuiproj.Marshal(p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成 SoapUI XML 失败: "+err.Error())
		return
	}
	name := p.Name
	if name == "" {
		name = "project"
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(name)+`-soapui-project.xml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// sanitizeFilename keeps filename-safe characters only.
func sanitizeFilename(s string) string {
	rep := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return rep.Replace(s)
}

func (s *Server) projectRoot(w http.ResponseWriter, r *http.Request, pid string) {
	switch r.Method {
	case http.MethodGet:
		p, err := s.Store.Get(pid)
		if err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPut:
		var p model.Project
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
			return
		}
		p.ID = pid
		if err := s.Store.UpdateProject(p); err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if err := s.Store.DeleteProject(pid); err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

func (s *Server) servicesRoute(w http.ResponseWriter, r *http.Request, pid string, rest []string) {
	// /projects/{pid}/services
	if len(rest) == 0 {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
			return
		}
		var svc model.Service
		if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
			return
		}
		saved, err := s.Store.AddService(pid, svc)
		if err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	sid := rest[0]
	// /projects/{pid}/services/{sid}
	if len(rest) == 1 {
		if r.Method != http.MethodDelete {
			writeErr(w, http.StatusMethodNotAllowed, "仅支持 DELETE")
			return
		}
		if err := s.Store.DeleteService(pid, sid); err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if rest[1] != "operations" {
		writeErr(w, http.StatusNotFound, "未知路径")
		return
	}
	s.operationsRoute(w, r, pid, sid, rest[2:])
}

func (s *Server) operationsRoute(w http.ResponseWriter, r *http.Request, pid, sid string, rest []string) {
	// /projects/{pid}/services/{sid}/operations
	if len(rest) == 0 {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
			return
		}
		var op model.Operation
		if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
			return
		}
		saved, err := s.Store.AddOperation(pid, sid, op)
		if err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	oid := rest[0]
	// /projects/{pid}/services/{sid}/operations/{oid}
	if len(rest) == 1 {
		if r.Method != http.MethodDelete {
			writeErr(w, http.StatusMethodNotAllowed, "仅支持 DELETE")
			return
		}
		if err := s.Store.DeleteOperation(pid, sid, oid); err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if rest[1] != "calls" {
		writeErr(w, http.StatusNotFound, "未知路径")
		return
	}
	s.callsRoute(w, r, pid, sid, oid, rest[2:])
}

func (s *Server) callsRoute(w http.ResponseWriter, r *http.Request, pid, sid, oid string, rest []string) {
	// /projects/{pid}/services/{sid}/operations/{oid}/calls
	if len(rest) == 0 {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
			return
		}
		var call model.Call
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
			return
		}
		saved, err := s.Store.AddCall(pid, sid, oid, call)
		if err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	cid := rest[0]
	// /projects/{pid}/services/{sid}/operations/{oid}/calls/{cid}
	switch r.Method {
	case http.MethodPut:
		var call model.Call
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
			return
		}
		call.ID = cid
		if err := s.Store.UpdateCall(pid, sid, oid, call); err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, call)
	case http.MethodDelete:
		if err := s.Store.DeleteCall(pid, sid, oid, cid); err != nil {
			mapStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}
