package workspace

import (
	"testing"

	"soapweb/internal/model"
)

// TestImportSplitsDuplicateProjectIDs is the regression test for importing a
// workspace file whose projects share an id.
//
// The store addresses projects by id alone, so duplicates made the second
// project unreachable: Get returned the first, and deleting the first made the
// second reappear on the next render — reported as "删除不生效".
func TestImportSplitsDuplicateProjectIDs(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	projects := []model.Project{
		{ID: "dup", Name: "第一个", Source: "native"},
		{ID: "dup", Name: "第二个", Source: "native"},
	}
	if _, err := reg.Import("导入", projects); err != nil {
		t.Fatalf("Import: %v", err)
	}

	st, _ := reg.Active()
	list := st.List()
	if len(list) != 2 {
		t.Fatalf("imported %d projects, want 2", len(list))
	}
	if list[0].ID == list[1].ID {
		t.Fatalf("both projects still share id %q", list[0].ID)
	}
	// The first occurrence keeps its id so nothing already referring to it breaks.
	if list[0].ID != "dup" {
		t.Errorf("first project id = %q, want the original dup kept", list[0].ID)
	}

	// Every project must resolve to itself, not to whichever came first.
	for _, want := range list {
		got, err := st.Get(want.ID)
		if err != nil {
			t.Fatalf("Get(%q): %v", want.ID, err)
		}
		if got.Name != want.Name {
			t.Errorf("Get(%q) = %q, want %q", want.ID, got.Name, want.Name)
		}
	}

	// Deleting one must leave exactly one: the old bug left the other one
	// "resurrecting" because it shared the deleted id.
	if err := st.DeleteProject(list[0].ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	rest := st.List()
	if len(rest) != 1 {
		t.Fatalf("after deleting one, %d projects remain: %+v", len(rest), rest)
	}
	if rest[0].Name != "第二个" {
		t.Errorf("remaining project = %q, want 第二个", rest[0].Name)
	}
}

// TestImportSplitsDuplicatesInsideOneProject covers the same collision inside a
// single project, where it also broke UpdateCall (the write landed on the first
// call sharing the id).
func TestImportSplitsDuplicatesInsideOneProject(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	projects := []model.Project{{
		ID:   "p",
		Name: "工程",
		Services: []model.Service{{
			ID: "s",
			Operations: []model.Operation{{
				ID: "o",
				Calls: []model.Call{
					{ID: "c", Title: "A", Request: "<a/>"},
					{ID: "c", Title: "B", Request: "<b/>"},
				},
			}},
		}},
	}}
	if _, err := reg.Import("导入", projects); err != nil {
		t.Fatalf("Import: %v", err)
	}

	st, _ := reg.Active()
	got, err := st.Get("p")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	calls := got.Services[0].Operations[0].Calls
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[0].ID == calls[1].ID {
		t.Fatalf("two calls still share id %q", calls[0].ID)
	}

	// The call that did not exist before the fix must now be addressable.
	if err := st.UpdateCall("p", "s", "o", model.Call{ID: calls[1].ID, Title: "B-改", Request: "<b/>"}); err != nil {
		t.Fatalf("UpdateCall: %v", err)
	}
	after, _ := st.Get("p")
	c := after.Services[0].Operations[0].Calls
	if c[0].Title != "A" {
		t.Errorf("updating the second call changed the first: %q", c[0].Title)
	}
	if c[1].Title != "B-改" {
		t.Errorf("second call = %q, want B-改", c[1].Title)
	}
}

// TestImportKeepsUniqueIDs makes sure the fix does not rewrite ids on a healthy
// file: changing them would invalidate every selection and saved link.
func TestImportKeepsUniqueIDs(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	projects := []model.Project{{
		ID:   "p1",
		Name: "工程一",
		Services: []model.Service{{
			ID:         "s1",
			Operations: []model.Operation{{ID: "o1", Calls: []model.Call{{ID: "c1"}}}},
		}},
	}, {
		ID:   "p2",
		Name: "工程二",
	}}
	if _, err := reg.Import("导入", projects); err != nil {
		t.Fatalf("Import: %v", err)
	}
	st, _ := reg.Active()
	got, err := st.Get("p1")
	if err != nil {
		t.Fatalf("Get(p1): %v", err)
	}
	if got.ID != "p1" || got.Services[0].ID != "s1" ||
		got.Services[0].Operations[0].ID != "o1" ||
		got.Services[0].Operations[0].Calls[0].ID != "c1" {
		t.Errorf("import rewrote ids on an already-unique workspace: %+v", got)
	}
	if _, err := st.Get("p2"); err != nil {
		t.Errorf("Get(p2): %v", err)
	}
}
