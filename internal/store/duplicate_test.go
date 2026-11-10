package store

import (
	"path/filepath"
	"testing"

	"soapweb/internal/model"
)

// testCall adds a project/service/operation and returns the ids plus a call.
func testCall(t *testing.T, s *Store) (string, string, string, model.Call) {
	t.Helper()
	p, err := s.AddProject(model.Project{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := s.AddService(p.ID, model.Service{Name: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.AddOperation(p.ID, svc.ID, model.Operation{Name: "op"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.AddCall(p.ID, svc.ID, op.ID, model.Call{Title: "查询", Request: "<q/>"})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID, svc.ID, op.ID, c
}

// TestDuplicateCallCopiesEverything checks the copy carries the payload, the
// headers and the timeout, gets a new id, and leaves the original untouched.
func TestDuplicateCallCopiesEverything(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	pid, sid, oid, c := testCall(t, s)

	original := model.Call{
		ID:        c.ID,
		Title:     c.Title,
		Request:   "<q>1</q>",
		Endpoint:  "http://host/svc",
		Action:    "urn:q",
		Headers:   []model.Header{{Name: "Authorization", Value: "Bearer t"}},
		TimeoutMS: 15000,
	}
	if err := s.UpdateCall(pid, sid, oid, original); err != nil {
		t.Fatal(err)
	}

	copied, err := s.DuplicateCall(pid, sid, oid, c.ID)
	if err != nil {
		t.Fatalf("DuplicateCall: %v", err)
	}
	if copied.ID == "" || copied.ID == c.ID {
		t.Errorf("copy kept the original id %q", copied.ID)
	}
	if copied.Title != "查询 (副本)" {
		t.Errorf("copy title = %q, want 查询 (副本)", copied.Title)
	}
	if copied.Request != original.Request || copied.Endpoint != original.Endpoint || copied.Action != original.Action {
		t.Errorf("copy lost payload fields: %+v", copied)
	}
	if copied.TimeoutMS != original.TimeoutMS {
		t.Errorf("copy timeout = %d, want %d", copied.TimeoutMS, original.TimeoutMS)
	}
	if len(copied.Headers) != 1 || copied.Headers[0].Value != "Bearer t" {
		t.Errorf("copy headers = %+v", copied.Headers)
	}

	stored, err := s.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	calls := stored.Services[0].Operations[0].Calls
	if len(calls) != 2 {
		t.Fatalf("operation has %d calls, want 2", len(calls))
	}
	if calls[0].Title != "查询" || calls[0].ID != c.ID {
		t.Errorf("original call was modified: %+v", calls[0])
	}

	// The copy must not share its header slice with the original.
	calls[1].Headers[0].Value = "changed"
	again, _ := s.Get(pid)
	if again.Services[0].Operations[0].Calls[0].Headers[0].Value != "Bearer t" {
		t.Error("copy shares header storage with the original")
	}
}

// TestDuplicateCallTitleCollision counts up instead of producing duplicates.
func TestDuplicateCallTitleCollision(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	pid, sid, oid, c := testCall(t, s)

	titles := []string{}
	for i := 0; i < 3; i++ {
		copied, err := s.DuplicateCall(pid, sid, oid, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		titles = append(titles, copied.Title)
	}
	want := []string{"查询 (副本)", "查询 (副本 2)", "查询 (副本 3)"}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("copy %d title = %q, want %q", i, titles[i], want[i])
		}
	}
}

func TestDuplicateCallNotFound(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	pid, sid, oid, c := testCall(t, s)

	if _, err := s.DuplicateCall(pid, sid, oid, "missing"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := s.DuplicateCall("nope", sid, oid, c.ID); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
