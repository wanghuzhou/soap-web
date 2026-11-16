package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"soapweb/internal/store"
	"soapweb/internal/workspace"
)

// newTestServer builds a Server backed by a fresh workspace registry whose
// "默认" workspace lives in a temp directory.
func newTestServer(t *testing.T) (*Server, *http.ServeMux, *store.Store) {
	t.Helper()
	srv, mux, reg := newTestServerWithRegistry(t)
	st, _ := reg.Active()
	return srv, mux, st
}

// newTestServerWithRegistry is newTestServer plus the registry, for tests that
// need to switch workspaces while a request is in flight.
func newTestServerWithRegistry(t *testing.T) (*Server, *http.ServeMux, *workspace.Registry) {
	t.Helper()
	reg, err := workspace.Open(t.TempDir(), "")
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	srv := &Server{Workspaces: reg}
	mux := http.NewServeMux()
	srv.Register(mux)
	return srv, mux, reg
}

// getRaw performs a GET and returns the raw body with the recorder, for tests
// that need the exact JSON shape rather than a decoded struct.
func getRaw(t *testing.T, mux *http.ServeMux, method, path string) ([]byte, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body, rec
}
