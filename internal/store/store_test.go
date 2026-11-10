package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"soapweb/internal/model"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestProjectCRUD(t *testing.T) {
	s, path := newTestStore(t)

	p, err := s.AddProject(model.Project{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" {
		t.Fatal("expected assigned id")
	}
	if got := len(s.List()); got != 1 {
		t.Fatalf("list len = %d", got)
	}

	// Reload from disk to prove persistence.
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != 1 || s2.List()[0].Name != "demo" {
		t.Fatalf("persisted data wrong: %+v", s2.List())
	}

	// Update.
	p.Name = "renamed"
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(p.ID); got.Name != "renamed" {
		t.Errorf("update failed: %q", got.Name)
	}

	// Delete.
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(p.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestNestedCRUD(t *testing.T) {
	s, _ := newTestStore(t)
	p, _ := s.AddProject(model.Project{Name: "p"})

	svc, err := s.AddService(p.ID, model.Service{Name: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.ID == "" {
		t.Fatal("service id missing")
	}
	op, err := s.AddOperation(p.ID, svc.ID, model.Operation{Name: "op"})
	if err != nil {
		t.Fatal(err)
	}
	call, err := s.AddCall(p.ID, svc.ID, op.ID, model.Call{Title: "c1", Request: "<a/>"})
	if err != nil {
		t.Fatal(err)
	}
	if call.ID == "" || call.Title != "c1" {
		t.Fatalf("call = %+v", call)
	}

	// Update call.
	call.Request = "<b/>"
	call.Title = "c1-renamed"
	if err := s.UpdateCall(p.ID, svc.ID, op.ID, call); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(p.ID)
	if got.Services[0].Operations[0].Calls[0].Title != "c1-renamed" {
		t.Errorf("call not updated")
	}

	// Delete call.
	if err := s.DeleteCall(p.ID, svc.ID, op.ID, call.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(p.ID)
	if len(got.Services[0].Operations[0].Calls) != 0 {
		t.Errorf("call not deleted")
	}

	// Delete operation, then service.
	if err := s.DeleteOperation(p.ID, svc.ID, op.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(p.ID)
	if len(got.Services[0].Operations) != 0 {
		t.Errorf("op not deleted")
	}
	if err := s.DeleteService(p.ID, svc.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(p.ID)
	if len(got.Services) != 0 {
		t.Errorf("service not deleted")
	}
}

func TestDeleteMissing(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.DeleteProject("nope"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteCall("p", "s", "o", "c"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAddCallSeedsTemplate(t *testing.T) {
	s, _ := newTestStore(t)
	// Operation carries a structural Template (方案 A) + endpoint + action.
	p, _ := s.AddProject(model.Project{
		Name: "p",
		Services: []model.Service{{
			Name: "svc",
			Operations: []model.Operation{{
				Name:       "Op",
				Endpoint:   "http://e",
				SoapAction: "urn:op",
				Request:    "<with-data>business</with-data>",
				Template:   "<skeleton><a></a></skeleton>",
			}},
		}},
	})
	svc := p.Services[0]
	op := svc.Operations[0]

	// Create a call with no request → should inherit the Template (not Request).
	c, err := s.AddCall(p.ID, svc.ID, op.ID, model.Call{Title: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Request != "<skeleton><a></a></skeleton>" {
		t.Errorf("request not seeded from Template: %q", c.Request)
	}
	if c.Endpoint != "http://e" {
		t.Errorf("endpoint not seeded: %q", c.Endpoint)
	}
	if c.Action != "urn:op" {
		t.Errorf("action not seeded: %q", c.Action)
	}

	// A call with an explicit request must NOT be overwritten.
	c2, err := s.AddCall(p.ID, svc.ID, op.ID, model.Call{Title: "x", Request: "<mine/>"})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Request != "<mine/>" {
		t.Errorf("explicit request overwritten: %q", c2.Request)
	}
}

func TestAssignIDsDeep(t *testing.T) {
	s, _ := newTestStore(t)
	p, err := s.AddProject(model.Project{
		Name: "deep",
		Services: []model.Service{{
			Name: "svc",
			Operations: []model.Operation{{
				Name:  "op",
				Calls: []model.Call{{Title: "c"}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(p.ID)
	if got.Services[0].ID == "" || got.Services[0].Operations[0].ID == "" || got.Services[0].Operations[0].Calls[0].ID == "" {
		t.Errorf("nested ids not assigned: %+v", got)
	}
}

// TestBackfillsLegacyWSDLCalls covers projects saved by an older build where a
// WSDL import produced operations but no request samples: loading must create
// one sample from the template (and stay idempotent on the next load).
func TestBackfillsLegacyWSDLCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	legacy := []model.Project{{
		ID:     "p1",
		Name:   "Legacy",
		Source: "wsdl-url",
		Services: []model.Service{{
			ID:   "s1",
			Name: "Svc",
			Operations: []model.Operation{{
				ID:       "o1",
				Name:     "Hello",
				Endpoint: "http://example.com/svc",
				Template: "<Envelope/>",
			}},
		}},
	}}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.List()[0]
	calls := got.Services[0].Operations[0].Calls
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1 backfilled sample", len(calls))
	}
	if calls[0].ID == "" || calls[0].Request != "<Envelope/>" || calls[0].Endpoint != "http://example.com/svc" {
		t.Errorf("backfilled call = %+v", calls[0])
	}

	// Reloading the persisted file must not add a second call.
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s2.List()[0].Services[0].Operations[0].Calls); n != 1 {
		t.Errorf("after reload calls = %d, want 1", n)
	}
}

// TestNewAcceptsUTF8BOM: a projects.json saved by an editor that adds a UTF-8
// BOM must load instead of failing with "invalid character '\ufeff'".
func TestNewAcceptsUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`[{"id":"p1","name":"bom"}]`)...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	list := s.List()
	if len(list) != 1 || list[0].Name != "bom" {
		t.Fatalf("projects = %+v", list)
	}
}

// TestUpdateProjectAssignsMissingIDs keeps UpdateProject consistent with
// AddProject: a full replace built by hand must still leave every node with an
// id, otherwise the node is persisted but no CRUD route can address it.
func TestUpdateProjectAssignsMissingIDs(t *testing.T) {
	s, _ := newTestStore(t)
	p, err := s.AddProject(model.Project{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}

	p.Services = []model.Service{{
		Name: "svc",
		Operations: []model.Operation{{
			Name:  "op",
			Calls: []model.Call{{Title: "c", Request: "<a/>"}},
		}},
	}}
	if err := s.UpdateProject(p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	got, err := s.Get(p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	svc := got.Services[0]
	op := svc.Operations[0]
	call := op.Calls[0]
	if svc.ID == "" || op.ID == "" || call.ID == "" {
		t.Fatalf("missing ids after UpdateProject: svc=%q op=%q call=%q", svc.ID, op.ID, call.ID)
	}
}
