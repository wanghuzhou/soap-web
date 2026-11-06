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
