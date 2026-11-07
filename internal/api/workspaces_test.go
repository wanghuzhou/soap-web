package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// workspaceInfo mirrors workspace.Info without importing it into every test
// struct literal.
type workspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
