package api

import (
	"errors"
	"net/http"

	"soapweb/internal/model"
	"soapweb/internal/store"
	"soapweb/internal/wsdl"
	"soapweb/internal/wsdlfetch"
)

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
	data, name, err := readUpload(w, r)
	if err != nil {
		writeUploadErr(w, err)
		return
	}
	service, err := wsdl.Parse(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "解析 WSDL 失败: "+err.Error())
		return
	}
	saved, err := s.persistWSDLProject(storeFrom(r), service, name, name, "wsdl")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存 WSDL 工程失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":     service,
		"project":     saved,
		"workspaceId": workspaceFrom(r).info.ID,
	})
}

// fetchWSDLRequest is the payload for POST /api/wsdl/fetch.
type fetchWSDLRequest struct {
	URL string `json:"url"`
	// Proxy overrides the download's proxy settings. When omitted, the server
	// defaults to a direct connection (the URL import dialog's default), unless
	// ProjectID names a project whose proxy should be reused.
	Proxy *model.ProxySettings `json:"proxy"`
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
	switch {
	case req.Proxy != nil:
		if err := req.Proxy.Validate(); err != nil {
			writeErr(w, http.StatusBadRequest, "代理配置无效: "+err.Error())
			return
		}
		proxyCfg = req.Proxy
	case req.ProjectID != "":
		if p, err := storeFrom(r).Get(req.ProjectID); err == nil {
			proxyCfg = p.Proxy
			if proxyCfg == nil {
				// A project without explicit settings means "system proxy".
				proxyCfg = &model.ProxySettings{Mode: "system"}
			}
		}
	}
	if proxyCfg == nil {
		// URL imports default to a direct connection; an unreachable/missing
		// project should not silently fall back to an unrelated system proxy.
		proxyCfg = &model.ProxySettings{Mode: "direct"}
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
	project, err := s.persistWSDLProject(storeFrom(r), service, fetched.Name, fetched.FinalURL, "wsdl-url")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存 WSDL 工程失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"service":     service,
		"project":     project,
		"url":         fetched.FinalURL,
		"workspaceId": workspaceFrom(r).info.ID,
	})
}

// persistWSDLProject saves a parsed WSDL service as a project. source records
// where the document came from (filename or final URL). Every operation gets
// one generated request sample, matching SoapUI's import behaviour.
func (s *Server) persistWSDLProject(st *store.Store, service *wsdl.Result, fallbackName, source, projectSource string) (model.Project, error) {
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
			if call, ok := model.SeedCall(op); ok {
				op.Calls = []model.Call{call}
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
	return st.AddProject(project)
}
