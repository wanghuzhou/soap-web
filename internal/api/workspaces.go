package api

import (
	"errors"
	"net/http"

	"soapweb/internal/workspace"
)

// registerWorkspaceRoutes declares the workspace management surface.
//
// The API only ever speaks ids and display names; the JSON files behind them
// are never exposed. That keeps the switcher a pure "pick a name" UI and lets a
// rename stay a metadata-only operation.
func (s *Server) registerWorkspaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces", s.handleListWorkspaces)
	mux.HandleFunc("POST /api/workspaces", s.handleCreateWorkspace)
	mux.HandleFunc("POST /api/workspaces/switch", s.handleSwitchWorkspace)
	mux.HandleFunc("PATCH /api/workspaces/{wid}", s.handleRenameWorkspace)
	mux.HandleFunc("DELETE /api/workspaces/{wid}", s.handleDeleteWorkspace)
}

// workspacesPayload is the complete state the frontend needs to render the
// switcher after any mutation.
type workspacesPayload struct {
	Current    workspace.Info   `json:"current"`
	Workspaces []workspace.Info `json:"workspaces"`
}

type workspaceNameRequest struct {
	Name string `json:"name"`
}

type workspaceSwitchRequest struct {
	ID string `json:"id"`
}

func (s *Server) workspacePayload() workspacesPayload {
	return workspacesPayload{
		Current:    s.Workspaces.CurrentInfo(),
		Workspaces: s.Workspaces.List(),
	}
}

// writeWorkspaces responds with the full list and stamps the response header
// with the *new* active workspace (the middleware's earlier value is stale
// after a switch).
func (s *Server) writeWorkspaces(w http.ResponseWriter) {
	payload := s.workspacePayload()
	w.Header().Set(workspaceHeader, payload.Current.ID)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, _ *http.Request) {
	s.writeWorkspaces(w)
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[workspaceNameRequest](w, r)
	if !ok {
		return
	}
	if _, err := s.Workspaces.Create(req.Name); err != nil {
		mapWorkspaceErr(w, err)
		return
	}
	s.writeWorkspaces(w)
}

func (s *Server) handleSwitchWorkspace(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[workspaceSwitchRequest](w, r)
	if !ok {
		return
	}
	if req.ID == "" {
		writeErr(w, http.StatusBadRequest, "缺少工作空间 id")
		return
	}
	if _, err := s.Workspaces.Switch(req.ID); err != nil {
		mapWorkspaceErr(w, err)
		return
	}
	s.writeWorkspaces(w)
}

func (s *Server) handleRenameWorkspace(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[workspaceNameRequest](w, r)
	if !ok {
		return
	}
	if _, err := s.Workspaces.Rename(r.PathValue("wid"), req.Name); err != nil {
		mapWorkspaceErr(w, err)
		return
	}
	s.writeWorkspaces(w)
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := s.Workspaces.Delete(r.PathValue("wid")); err != nil {
		mapWorkspaceErr(w, err)
		return
	}
	s.writeWorkspaces(w)
}

// mapWorkspaceErr converts registry errors into HTTP responses.
func mapWorkspaceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, workspace.ErrInvalidName),
		errors.Is(err, workspace.ErrDuplicateName),
		errors.Is(err, workspace.ErrLastWorkspace):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
