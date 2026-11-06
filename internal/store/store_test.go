package store

import (
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
	// Operation carries a template + endpoint + action.
	p, _ := s.AddProject(model.Project{
		Name: "p",
		Services: []model.Service{{
			Name: "svc",
			Operations: []model.Operation{{
				Name:       "Op",
				Endpoint:   "http://e",
				SoapAction: "urn:op",
				Request:    "<template/>",
			}},
		}},
	})
	svc := p.Services[0]
	op := svc.Operations[0]

	// Create a call with no request → should inherit template/endpoint/action.
	c, err := s.AddCall(p.ID, svc.ID, op.ID, model.Call{Title: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Request != "<template/>" {
		t.Errorf("request not seeded: %q", c.Request)
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
