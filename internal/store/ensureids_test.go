package store

import (
	"path/filepath"
	"testing"

	"soapweb/internal/model"
)

// collectIDs walks a project and returns every id it finds, in tree order.
func collectIDs(p model.Project) map[string]int {
	counts := map[string]int{}
	counts[p.ID]++
	for _, svc := range p.Services {
		counts[svc.ID]++
		for _, op := range svc.Operations {
			counts[op.ID]++
			for _, c := range op.Calls {
				counts[c.ID]++
			}
		}
	}
	return counts
}

// TestEnsureIDsSplitsDuplicates pins the invariant that made duplicate ids a
// data-loss bug: every lookup takes the first match on an id, so a second node
// sharing it becomes unreachable.
func TestEnsureIDsSplitsDuplicates(t *testing.T) {
	p := model.Project{
		ID: "dup",
		Services: []model.Service{{
			ID: "dup",
			Operations: []model.Operation{{
				ID: "opsame",
				Calls: []model.Call{
					{ID: "callsame", Title: "A"},
					{ID: "callsame", Title: "B"},
					{ID: "", Title: "需要补 id"},
				},
			}, {
				ID: "opsame",
				Calls: []model.Call{
					{ID: "callsame", Title: "C"},
				},
			}},
		}, {
			ID: "dup",
		}},
	}

	EnsureIDs(&p)

	for id, n := range collectIDs(p) {
		if n > 1 {
			t.Errorf("id %q still appears %d times after EnsureIDs", id, n)
		}
		if id == "" {
			t.Errorf("EnsureIDs left an empty id")
		}
	}

	// The first occurrence keeps its id, so anything referring to it (a UI
	// selection, a URL already open) still resolves.
	if p.ID != "dup" {
		t.Errorf("project id = %q, want the first occurrence kept as dup", p.ID)
	}
	if p.Services[0].ID != "dup" && p.Services[0].ID != "" {
		// The project already claimed "dup", so the service must have been
		// reissued; assert it differs rather than a specific value.
		if p.Services[0].ID == p.ID {
			t.Errorf("service reused the project id %q", p.ID)
		}
	}
	if p.Services[0].Operations[0].ID != "opsame" {
		t.Errorf("first operation id = %q, want opsame kept", p.Services[0].Operations[0].ID)
	}
	if p.Services[0].Operations[1].ID == "opsame" {
		t.Errorf("second operation kept the colliding id opsame")
	}
	if got := p.Services[0].Operations[0].Calls[0].ID; got != "callsame" {
		t.Errorf("first call id = %q, want callsame kept", got)
	}
}

// TestEnsureIDsIsStableForUniqueTrees makes sure the fix does not churn ids on
// a healthy project: reassigning them would break every open tab's selection.
func TestEnsureIDsIsStableForUniqueTrees(t *testing.T) {
	p := model.Project{
		ID: "p1",
		Services: []model.Service{{
			ID: "s1",
			Operations: []model.Operation{{
				ID:    "o1",
				Calls: []model.Call{{ID: "c1"}, {ID: "c2"}},
			}},
		}},
	}
	EnsureIDs(&p)
	if p.ID != "p1" || p.Services[0].ID != "s1" ||
		p.Services[0].Operations[0].ID != "o1" ||
		p.Services[0].Operations[0].Calls[0].ID != "c1" ||
		p.Services[0].Operations[0].Calls[1].ID != "c2" {
		t.Errorf("EnsureIDs changed ids on an already-unique tree: %+v", p)
	}
}

// TestUpdateProjectSplitsDuplicateCallIDs reproduces the concrete failure: a
// client PUTs a project whose two calls share an id, then a later update must
// reach the right call instead of overwriting the first one.
func TestUpdateProjectSplitsDuplicateCallIDs(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := model.Project{
		ID: "p1",
		Services: []model.Service{{
			ID: "s1",
			Operations: []model.Operation{{
				ID: "o1",
				Calls: []model.Call{
					{ID: "same", Title: "B", Request: "<b/>"},
					{ID: "same", Title: "C", Request: "<c/>"},
				},
			}},
		}},
	}
	// UpdateProject replaces an existing project, so create it first.
	if _, err := s.AddProject(model.Project{ID: "p1", Name: "p1"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := s.UpdateProject(p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	stored, err := s.Get("p1")
	if err != nil {
		t.Fatal(err)
	}
	calls := stored.Services[0].Operations[0].Calls
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[0].ID == calls[1].ID {
		t.Fatalf("two calls still share id %q after UpdateProject", calls[0].ID)
	}

	// Both calls must now be independently addressable.
	if err := s.UpdateCall("p1", "s1", "o1", model.Call{ID: calls[1].ID, Title: "C-改", Request: "<c/>"}); err != nil {
		t.Fatalf("UpdateCall on the second call: %v", err)
	}
	after, _ := s.Get("p1")
	got := after.Services[0].Operations[0].Calls
	if got[0].Title != "B" {
		t.Errorf("updating the second call changed the first: %q", got[0].Title)
	}
	if got[1].Title != "C-改" {
		t.Errorf("second call title = %q, want C-改", got[1].Title)
	}
}

// TestAddServiceSplitsDuplicateIDs checks the insert paths, which only need the
// inserted subtree to be self-consistent.
func TestAddServiceSplitsDuplicateIDs(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AddProject(model.Project{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := s.AddService(p.ID, model.Service{
		ID: "same",
		Operations: []model.Operation{{
			ID:    "same",
			Calls: []model.Call{{ID: "same"}, {ID: "same"}},
		}},
	})
	if err != nil {
		t.Fatalf("AddService: %v", err)
	}
	ids := map[string]bool{}
	for _, id := range []string{svc.ID, svc.Operations[0].ID, svc.Operations[0].Calls[0].ID, svc.Operations[0].Calls[1].ID} {
		if ids[id] {
			t.Errorf("duplicate id %q survived AddService", id)
		}
		ids[id] = true
	}
}
