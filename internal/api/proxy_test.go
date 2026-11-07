package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"soapweb/internal/model"
)

// newProxyTestServer builds a Server backed by a temp store holding one project.
func newProxyTestServer(t *testing.T) (*Server, *http.ServeMux, model.Project) {
	t.Helper()
	srv, mux, st := newTestServer(t)
	proj, err := st.AddProject(model.Project{
		Name: "P",
		Services: []model.Service{{
			ID:   "s1",
			Name: "S",
			Operations: []model.Operation{{
				ID:    "o1",
				Name:  "O",
				Calls: []model.Call{{ID: "c1", Title: "t", Request: "<x/>"}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	return srv, mux, proj
}

// TestUpdateProjectProxyNarrowWrite 回归：改代理曾通过 PUT /api/projects/{pid}
// 提交整棵工程树，会覆盖其他标签页的并发修改。窄接口只写 proxy 字段。
func TestUpdateProjectProxyNarrowWrite(t *testing.T) {
	_, mux, proj := newProxyTestServer(t)

	body, _ := json.Marshal(model.ProxySettings{
		Mode: "custom", Type: "http", Host: "127.0.0.1", Port: 8080,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+proj.ID+"/proxy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// 工程的其他部分必须原样保留（这是"窄写"的意义所在）。
	gotReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+proj.ID, nil)
	gotW := httptest.NewRecorder()
	mux.ServeHTTP(gotW, gotReq)

	var got model.Project
	if err := json.Unmarshal(gotW.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Proxy == nil || got.Proxy.Host != "127.0.0.1" || got.Proxy.Port != 8080 {
		t.Fatalf("proxy 未保存: %+v", got.Proxy)
	}
	if len(got.Services) != 1 || len(got.Services[0].Operations) != 1 {
		t.Fatalf("窄写破坏了工程结构: %+v", got.Services)
	}
	if len(got.Services[0].Operations[0].Calls) != 1 {
		t.Fatalf("窄写破坏了请求样例: %+v", got.Services[0].Operations[0].Calls)
	}
}

// TestUpdateProjectProxyRejectsInvalid 无效代理配置应被拒绝。
func TestUpdateProjectProxyRejectsInvalid(t *testing.T) {
	_, mux, proj := newProxyTestServer(t)

	cases := []model.ProxySettings{
		{Mode: "custom", Type: "http", Host: "", Port: 8080},
		{Mode: "custom", Type: "http", Host: "127.0.0.1", Port: 0},
		{Mode: "custom", Type: "ftp", Host: "127.0.0.1", Port: 8080},
		{Mode: "bogus"},
	}
	for i, c := range cases {
		body, _ := json.Marshal(c)
		req := httptest.NewRequest(http.MethodPut, "/api/projects/"+proj.ID+"/proxy", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("case %d: status = %d, want 400 (body=%s)", i, w.Code, w.Body.String())
		}
	}
}

// TestUpdateProjectProxyUnknownProject 找不到工程时返回 404。
func TestUpdateProjectProxyUnknownProject(t *testing.T) {
	_, mux, _ := newProxyTestServer(t)
	body, _ := json.Marshal(model.ProxySettings{Mode: "direct"})
	req := httptest.NewRequest(http.MethodPut, "/api/projects/nope/proxy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestUpdateProjectProxyBadBody 非法 JSON 返回 400。
func TestUpdateProjectProxyBadBody(t *testing.T) {
	_, mux, proj := newProxyTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+proj.ID+"/proxy",
		bytes.NewReader([]byte("{not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestListProjectsDoesNotLeakInternalState 回归：List 返回值曾被篡改后
// 影响 store 内部状态。HTTP 层验证一次，确保 handler 拿到的确实是副本。
func TestListProjectsDoesNotLeakInternalState(t *testing.T) {
	st, mux, proj := newProxyTestServer(t)
	_ = st

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var list []model.Project
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || list[0].ID != proj.ID {
		t.Fatalf("list = %+v", list)
	}
}
