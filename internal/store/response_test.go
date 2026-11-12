package store

import (
	"testing"

	"soapweb/internal/model"
)

// TestUpdateCallResponsePersists covers the saved response lifecycle: it
// survives a reload, is deep-copied, and is not erased by a request edit.
func TestUpdateCallResponsePersists(t *testing.T) {
	s, path := newTestStore(t)

	project, err := s.AddProject(model.Project{
		Name: "p",
		Services: []model.Service{{
			Name: "svc",
			Operations: []model.Operation{{
				Name: "op",
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pid := project.ID
	sid := project.Services[0].ID
	oid := project.Services[0].Operations[0].ID

	call, err := s.AddCall(pid, sid, oid, model.Call{Title: "c", Request: "<a/>"})
	if err != nil {
		t.Fatal(err)
	}

	resp := &model.SavedResponse{
		Status:     200,
		StatusText: "OK",
		Headers:    map[string]string{"Content-Type": "text/xml"},
		Body:       "<ok/>",
		DurationMS: 5,
		Size:       5,
	}
	if err := s.UpdateCallResponse(pid, sid, oid, call.ID, resp); err != nil {
		t.Fatal(err)
	}

	// Mutating the caller's value must not reach the store.
	resp.Headers["X-Mutated"] = "1"
	got, err := s.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	saved := got.Services[0].Operations[0].Calls[0].Response
	if saved == nil || saved.Status != 200 || saved.Body != "<ok/>" {
		t.Fatalf("response not saved: %+v", saved)
	}
	if _, ok := saved.Headers["X-Mutated"]; ok {
		t.Error("saved response shares its header map with the caller")
	}

	// Reload from disk: the response must be part of the workspace JSON.
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := s2.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := got2.Services[0].Operations[0].Calls[0]
	if reloaded.Response == nil || reloaded.Response.Body != "<ok/>" {
		t.Fatalf("response not persisted: %+v", reloaded.Response)
	}

	// A request edit (which omits the response) must preserve it.
	reloaded.Request = "<b/>"
	reloaded.Response = nil
	if err := s2.UpdateCall(pid, sid, oid, reloaded); err != nil {
		t.Fatal(err)
	}
	got3, err := s2.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	edited := got3.Services[0].Operations[0].Calls[0]
	if edited.Request != "<b/>" {
		t.Errorf("request edit lost: %q", edited.Request)
	}
	if edited.Response == nil || edited.Response.Body != "<ok/>" {
		t.Errorf("request edit erased the saved response: %+v", edited.Response)
	}

	// A newer response replaces the previous one: sending again and saving must
	// update the sample rather than being blocked by the existing response.
	if err := s2.UpdateCallResponse(pid, sid, oid, call.ID, &model.SavedResponse{
		Status: 500, StatusText: "Server Error", Body: "<err/>",
	}); err != nil {
		t.Fatal(err)
	}
	got5, err := s2.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	updated := got5.Services[0].Operations[0].Calls[0].Response
	if updated == nil || updated.Status != 500 || updated.Body != "<err/>" {
		t.Errorf("new response did not replace the saved one: %+v", updated)
	}

	// Passing nil clears it.
	if err := s2.UpdateCallResponse(pid, sid, oid, call.ID, nil); err != nil {
		t.Fatal(err)
	}
	got4, err := s2.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	if got4.Services[0].Operations[0].Calls[0].Response != nil {
		t.Error("nil response should clear the saved response")
	}
}

// TestDuplicateCallCopiesResponseIndependently makes sure the copy owns its
// header map rather than sharing it with the original.
func TestDuplicateCallCopiesResponseIndependently(t *testing.T) {
	s, _ := newTestStore(t)
	project, err := s.AddProject(model.Project{
		Name: "p",
		Services: []model.Service{{
			Name:       "svc",
			Operations: []model.Operation{{Name: "op"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pid := project.ID
	sid := project.Services[0].ID
	oid := project.Services[0].Operations[0].ID
	call, err := s.AddCall(pid, sid, oid, model.Call{Title: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCallResponse(pid, sid, oid, call.ID, &model.SavedResponse{
		Status: 200, Headers: map[string]string{"A": "1"}, Body: "<ok/>",
	}); err != nil {
		t.Fatal(err)
	}

	dup, err := s.DuplicateCall(pid, sid, oid, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Response == nil || dup.Response.Body != "<ok/>" {
		t.Fatalf("duplicate lost the response: %+v", dup.Response)
	}
	dup.Response.Headers["A"] = "2"

	got, err := s.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	original := got.Services[0].Operations[0].Calls[0].Response
	if original.Headers["A"] != "1" {
		t.Errorf("duplicate shares its header map with the original: %+v", original.Headers)
	}
}
