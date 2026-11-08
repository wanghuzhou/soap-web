package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"soapweb/internal/model"
)

// workspaceRequest performs an API call, optionally with a workspace header.
func workspaceRequest(t *testing.T, mux *http.ServeMux, method, path string, payload any, workspaceID string) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if workspaceID != "" {
		req.Header.Set(workspaceHeader, workspaceID)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeWorkspaces(t *testing.T, rec *httptest.ResponseRecorder) workspacesPayload {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload workspacesPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return payload
}

// TestWorkspaceLifecycleHTTP walks list → create → switch → rename → delete and
// verifies that project data follows the active workspace.
func TestWorkspaceLifecycleHTTP(t *testing.T) {
	_, mux, _ := newTestServer(t)

	initial := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, ""))
	if initial.Current.Name != "默认" || len(initial.Workspaces) != 1 {
		t.Fatalf("initial = %+v", initial)
	}
	defaultID := initial.Current.ID

	created := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodPost, "/api/workspaces",
		map[string]string{"name": "测试空间"}, ""))
	if created.Current.Name != "测试空间" || len(created.Workspaces) != 2 {
		t.Fatalf("created = %+v", created)
	}
	otherID := created.Current.ID

	// A project created now belongs to "测试空间" only.
	if rec := workspaceRequest(t, mux, http.MethodPost, "/api/projects",
		model.Project{Name: "in-other"}, otherID); rec.Code != http.StatusOK {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}

	switched := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodPost, "/api/workspaces/switch",
		map[string]string{"id": defaultID}, ""))
	if switched.Current.ID != defaultID {
		t.Fatalf("switch = %+v", switched)
	}
	var projects []model.Project
	if rec := workspaceRequest(t, mux, http.MethodGet, "/api/projects", nil, defaultID); rec.Code != http.StatusOK {
		t.Fatalf("list projects: %d %s", rec.Code, rec.Body.String())
	} else if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("default workspace should be empty, got %+v", projects)
	}

	if rec := workspaceRequest(t, mux, http.MethodGet, "/api/projects", nil, otherID); rec.Code != http.StatusConflict {
		t.Errorf("stale read status = %d, want 409", rec.Code)
	}

	renamed := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodPatch, "/api/workspaces/"+defaultID,
		map[string]string{"name": "主空间"}, ""))
	if renamed.Current.Name != "主空间" {
		t.Fatalf("renamed = %+v", renamed)
	}

	// Deleting the active workspace falls back to the other one.
	deleted := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodDelete, "/api/workspaces/"+defaultID, nil, ""))
	if deleted.Current.ID != otherID || len(deleted.Workspaces) != 1 {
		t.Fatalf("deleted = %+v", deleted)
	}

	// The last workspace cannot be deleted.
	if rec := workspaceRequest(t, mux, http.MethodDelete, "/api/workspaces/"+otherID, nil, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("delete last status = %d, want 400", rec.Code)
	}
}

// TestStaleWorkspaceRejected covers the multi-tab guard: a tab still pointing
// at the previous workspace must not read or write the new one.
func TestStaleWorkspaceRejected(t *testing.T) {
	_, mux, _ := newTestServer(t)

	initial := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, ""))
	oldID := initial.Current.ID
	created := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodPost, "/api/workspaces",
		map[string]string{"name": "另一个"}, ""))
	newID := created.Current.ID

	rec := workspaceRequest(t, mux, http.MethodGet, "/api/projects", nil, oldID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale request status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Code      string        `json:"code"`
		Workspace workspaceInfo `json:"workspace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Code != "workspace_mismatch" {
		t.Errorf("code = %q", conflict.Code)
	}
	if conflict.Workspace.ID != newID {
		t.Errorf("conflict workspace = %+v, want %s", conflict.Workspace, newID)
	}

	// Health stays reachable from a stale tab and reports the new workspace.
	health := workspaceRequest(t, mux, http.MethodGet, "/api/health", nil, oldID)
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}
	if got := health.Header().Get(workspaceHeader); got != newID {
		t.Errorf("health header = %q, want %q", got, newID)
	}
	var healthBody struct {
		Status    string        `json:"status"`
		Workspace workspaceInfo `json:"workspace"`
	}
	if err := json.Unmarshal(health.Body.Bytes(), &healthBody); err != nil {
		t.Fatal(err)
	}
	if healthBody.Status != "ok" || healthBody.Workspace.ID != newID {
		t.Errorf("health = %+v", healthBody)
	}

	// The workspace endpoints themselves must not be blocked.
	if rec := workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, oldID); rec.Code != http.StatusOK {
		t.Errorf("workspace list from stale tab = %d, want 200", rec.Code)
	}
}

// TestImportResponseCarriesWorkspaceID documents where a long import landed
// when the workspace changed while it was in flight.
func TestImportResponseCarriesWorkspaceID(t *testing.T) {
	_, mux, _ := newTestServer(t)
	current := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, "")).Current

	req := httptest.NewRequest(http.MethodPost, "/api/wsdl/parse", bytes.NewReader([]byte(remoteWSDLForURLImport)))
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set(workspaceHeader, current.ID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		WorkspaceID string `json:"workspaceId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.WorkspaceID != current.ID {
		t.Errorf("workspaceId = %q, want %q", resp.WorkspaceID, current.ID)
	}
}

// TestImportStaysInWorkspaceItStartedIn covers the long-operation rule: a WSDL
// fetch pins the workspace at request start, so switching while the download is
// in flight must not move the result into the new workspace.
func TestImportStaysInWorkspaceItStartedIn(t *testing.T) {
	_, mux, reg := newTestServerWithRegistry(t)
	first := reg.CurrentInfo()

	second, err := reg.Create("另一个")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := reg.Switch(first.ID); err != nil {
		t.Fatalf("Switch back: %v", err)
	}
	firstStore, _ := reg.Active()

	started := make(chan struct{})
	release := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
		_, _ = w.Write([]byte(remoteWSDLForURLImport))
	}))
	defer remote.Close()

	body, _ := json.Marshal(map[string]string{"url": remote.URL + "/svc.wsdl"})
	req := httptest.NewRequest(http.MethodPost, "/api/wsdl/fetch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(workspaceHeader, first.ID)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rec, req)
	}()

	<-started
	if _, err := reg.Switch(second.ID); err != nil {
		t.Fatalf("Switch during import: %v", err)
	}
	close(release)
	<-done

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		WorkspaceID string `json:"workspaceId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.WorkspaceID != first.ID {
		t.Errorf("workspaceId = %q, want the workspace the import started in (%q)", resp.WorkspaceID, first.ID)
	}
	if got := rec.Header().Get(workspaceHeader); got != first.ID {
		t.Errorf("response header = %q, want %q", got, first.ID)
	}
	if got := firstStore.List(); len(got) != 1 {
		t.Errorf("imported project should be in the starting workspace, got %d", len(got))
	}
	secondStore, _ := reg.Active()
	if got := secondStore.List(); len(got) != 0 {
		t.Errorf("switched-to workspace must stay empty, got %d", len(got))
	}
}

// TestExportWorkspaceProxyOption covers the export switch: proxies are dropped
// by default and preserved when keepProxy is true.
func TestExportWorkspaceProxyOption(t *testing.T) {
	_, mux, st := newTestServer(t)
	current := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, "")).Current
	project, err := st.AddProject(model.Project{
		Name:  "with-proxy",
		Proxy: &model.ProxySettings{Mode: "custom", Type: "http", Host: "127.0.0.1", Port: 8080},
		Services: []model.Service{{
			Name:       "svc",
			Operations: []model.Operation{{Name: "op"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	export := func(keep bool) []model.Project {
		t.Helper()
		rec := workspaceRequest(t, mux, http.MethodPost, "/api/workspaces/export",
			map[string]bool{"keepProxy": keep}, current.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("export keepProxy=%v: %d %s", keep, rec.Code, rec.Body.String())
		}
		var projects []model.Project
		if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
			t.Fatal(err)
		}
		return projects
	}

	without := export(false)
	if len(without) != 1 {
		t.Fatalf("exported projects = %d, want 1", len(without))
	}
	if without[0].Proxy != nil {
		t.Errorf("proxy should be dropped by default: %+v", without[0].Proxy)
	}
	if without[0].ID != project.ID || without[0].Name != "with-proxy" {
		t.Errorf("project identity lost: %+v", without[0])
	}
	if len(without[0].Services) != 1 || without[0].Services[0].Operations[0].Name != "op" {
		t.Errorf("project content lost: %+v", without[0].Services)
	}

	with := export(true)
	if with[0].Proxy == nil || with[0].Proxy.Host != "127.0.0.1" || with[0].Proxy.Port != 8080 {
		t.Errorf("proxy should be preserved: %+v", with[0].Proxy)
	}
}

// TestImportWorkspace exports a project and imports it as a new workspace.
func TestImportWorkspace(t *testing.T) {
	_, mux, st := newTestServer(t)
	current := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, "")).Current
	project, err := st.AddProject(model.Project{
		Name:     "imported",
		Proxy:    &model.ProxySettings{Mode: "direct"},
		Services: []model.Service{{Name: "svc"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	exportRec := workspaceRequest(t, mux, http.MethodPost, "/api/workspaces/export",
		map[string]bool{"keepProxy": true}, current.ID)
	var projects []model.Project
	if err := json.Unmarshal(exportRec.Body.Bytes(), &projects); err != nil {
		t.Fatal(err)
	}

	rec := workspaceRequest(t, mux, http.MethodPost,
		"/api/workspaces/import?name="+url.QueryEscape("导入空间"), projects, current.ID)
	payload := decodeWorkspaces(t, rec)
	if payload.Current.Name != "导入空间" {
		t.Errorf("current = %+v", payload.Current)
	}
	if len(payload.Workspaces) != 2 {
		t.Errorf("workspaces = %+v", payload.Workspaces)
	}

	var imported []model.Project
	listRec := workspaceRequest(t, mux, http.MethodGet, "/api/projects", nil, payload.Current.ID)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list imported: %d %s", listRec.Code, listRec.Body.String())
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0].Name != "imported" {
		t.Fatalf("imported projects = %+v", imported)
	}
	if imported[0].ID != project.ID || imported[0].Proxy == nil || imported[0].Proxy.Mode != "direct" {
		t.Errorf("imported project lost id/proxy: %+v", imported[0])
	}

	// The same name cannot be imported twice.
	dup := workspaceRequest(t, mux, http.MethodPost,
		"/api/workspaces/import?name="+url.QueryEscape("导入空间"), projects, payload.Current.ID)
	if dup.Code != http.StatusBadRequest {
		t.Errorf("duplicate import status = %d, want 400", dup.Code)
	}
}

// TestImportWorkspaceRejectsInvalidData covers malformed JSON, invalid proxy
// settings and stale-tab exports.
func TestImportWorkspaceRejectsInvalidData(t *testing.T) {
	_, mux, _ := newTestServer(t)
	current := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodGet, "/api/workspaces", nil, "")).Current

	badJSON := httptest.NewRequest(http.MethodPost, "/api/workspaces/import?name=bad", strings.NewReader("{not json"))
	badJSON.Header.Set("Content-Type", "application/json")
	badJSON.Header.Set(workspaceHeader, current.ID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, badJSON)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON status = %d, want 400", rec.Code)
	}

	badProxy := []model.Project{{
		Name:  "bad",
		Proxy: &model.ProxySettings{Mode: "custom", Type: "http", Host: "", Port: 0},
	}}
	rec = workspaceRequest(t, mux, http.MethodPost, "/api/workspaces/import?name=bad-proxy", badProxy, current.ID)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid proxy status = %d, want 400", rec.Code)
	}

	// Create a second workspace, then try to export with the old id.
	other := decodeWorkspaces(t, workspaceRequest(t, mux, http.MethodPost, "/api/workspaces",
		map[string]string{"name": "另一个"}, ""))
	rec = workspaceRequest(t, mux, http.MethodPost, "/api/workspaces/export",
		map[string]bool{"keepProxy": false}, current.ID)
	if rec.Code != http.StatusConflict {
		t.Errorf("stale export status = %d, want 409 (current is %s)", rec.Code, other.Current.ID)
	}
}

// workspaceInfo mirrors workspace.Info without importing it into every test
// struct literal.
type workspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
