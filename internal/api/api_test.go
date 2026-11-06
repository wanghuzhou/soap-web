package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"soapweb/internal/model"
	"soapweb/internal/store"
)

// TestSendUsesProjectProxy verifies that /api/send resolves the project's proxy
// settings and routes the request through the configured proxy.
func TestSendUsesProjectProxy(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}

	// A fake HTTP proxy that answers every request with a marker body.
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "" {
			t.Errorf("proxy expected absolute URI, got %v", r.URL)
		}
		_, _ = w.Write([]byte("via-project-proxy"))
	}))
	defer proxySrv.Close()
	pu, _ := url.Parse(proxySrv.URL)

	proj, err := st.AddProject(model.Project{
		Name: "p",
		Proxy: &model.ProxySettings{
			Mode: "custom", Type: "http", Host: pu.Hostname(), Port: portOf(pu),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := &Server{Store: st}
	mux := http.NewServeMux()
	srv.Register(mux)

	body, _ := json.Marshal(model.SendRequest{
		ProjectID: proj.ID,
		URL:       "http://target.invalid/x",
		Method:    "GET",
		TimeoutMS: 5000,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp model.SendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" {
		t.Fatalf("send error: %s", resp.Error)
	}
	if resp.Body != "via-project-proxy" {
		t.Errorf("body = %q (proxy not used?)", resp.Body)
	}
}

// TestSaveProjectRejectsInvalidProxy verifies validation on project update.
func TestSaveProjectRejectsInvalidProxy(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.New(filepath.Join(dir, "projects.json"))
	p, _ := st.AddProject(model.Project{Name: "p"})

	srv := &Server{Store: st}
	mux := http.NewServeMux()
	srv.Register(mux)

	bad := *p2ptr(p)
	bad.Proxy = &model.ProxySettings{Mode: "custom", Type: "http", Host: "", Port: 0}
	body, _ := json.Marshal(bad)
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+p.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestNestedProjectCRUDRoutes(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{Store: st}
	mux := http.NewServeMux()
	srv.Register(mux)

	request := func(method, path string, payload any, out any) *httptest.ResponseRecorder {
		t.Helper()
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if out != nil && rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatalf("%s %s: decode response: %v", method, path, err)
			}
		}
		return rec
	}

	var project model.Project
	if rec := request(http.MethodPost, "/api/projects", model.Project{Name: "p"}, &project); rec.Code != http.StatusOK {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}

	var service model.Service
	if rec := request(http.MethodPost, "/api/projects/"+project.ID+"/services", model.Service{Name: "svc"}, &service); rec.Code != http.StatusOK {
		t.Fatalf("add service: %d %s", rec.Code, rec.Body.String())
	}

	var operation model.Operation
	if rec := request(http.MethodPost, "/api/projects/"+project.ID+"/services/"+service.ID+"/operations", model.Operation{Name: "op"}, &operation); rec.Code != http.StatusOK {
		t.Fatalf("add operation: %d %s", rec.Code, rec.Body.String())
	}

	var call model.Call
	if rec := request(http.MethodPost, "/api/projects/"+project.ID+"/services/"+service.ID+"/operations/"+operation.ID+"/calls", model.Call{Title: "c1"}, &call); rec.Code != http.StatusOK {
		t.Fatalf("add call: %d %s", rec.Code, rec.Body.String())
	}

	call.Title = "renamed"
	if rec := request(http.MethodPut, "/api/projects/"+project.ID+"/services/"+service.ID+"/operations/"+operation.ID+"/calls/"+call.ID, call, nil); rec.Code != http.StatusOK {
		t.Fatalf("update call: %d %s", rec.Code, rec.Body.String())
	}
	got, err := st.Get(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Services[0].Operations[0].Calls[0].Title != "renamed" {
		t.Errorf("call title = %q", got.Services[0].Operations[0].Calls[0].Title)
	}

	if rec := request(http.MethodDelete, "/api/projects/"+project.ID+"/services/"+service.ID+"/operations/"+operation.ID+"/calls/"+call.ID, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete call: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request(http.MethodGet, "/api/projects/"+project.ID, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("get project: %d %s", rec.Code, rec.Body.String())
	}
}

func p2ptr(p model.Project) *model.Project { return &p }

func portOf(u *url.URL) int {
	p := u.Port()
	n := 0
	for _, c := range p {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
