package api

import (
	"net/http"
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
