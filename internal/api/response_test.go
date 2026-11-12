package api

import (
	"net/http"
	"testing"

	"soapweb/internal/model"
)

// TestCallResponseEndpoint covers the narrow response write: it persists, comes
// back with the project, and a JSON null clears it.
func TestCallResponseEndpoint(t *testing.T) {
	_, mux, st := newTestServer(t)

	project, err := st.AddProject(model.Project{
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
	call, err := st.AddCall(pid, sid, oid, model.Call{Title: "c", Request: "<a/>"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/projects/" + pid + "/services/" + sid
	path += "/operations/" + oid + "/calls/" + call.ID + "/response"

	saved := model.SavedResponse{
		Status:     200,
		StatusText: "OK",
		Headers:    map[string]string{"Content-Type": "text/xml"},
		Body:       "<ok/>",
		DurationMS: 7,
		Size:       5,
	}
	if rec := sendJSON(t, mux, http.MethodPut, path, saved, nil); rec.Code != http.StatusOK {
		t.Fatalf("save response: %d %s", rec.Code, rec.Body.String())
	}

	var loaded model.Project
	if rec := sendJSON(t, mux, http.MethodGet, "/api/projects/"+pid, nil, &loaded); rec.Code != http.StatusOK {
		t.Fatalf("get project: %d %s", rec.Code, rec.Body.String())
	}
	got := loaded.Services[0].Operations[0].Calls[0].Response
	if got == nil || got.Status != 200 || got.Body != "<ok/>" || got.DurationMS != 7 {
		t.Fatalf("saved response not returned: %+v", got)
	}

	// A request edit through the normal call endpoint must not erase it.
	edited := loaded.Services[0].Operations[0].Calls[0]
	edited.Request = "<b/>"
	edited.Response = nil
	callPath := "/api/projects/" + pid + "/services/" + sid
	callPath += "/operations/" + oid + "/calls/" + call.ID
	if rec := sendJSON(t, mux, http.MethodPut, callPath, edited, nil); rec.Code != http.StatusOK {
		t.Fatalf("update call: %d %s", rec.Code, rec.Body.String())
	}
	gotProject, err := st.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	editedSaved := gotProject.Services[0].Operations[0].Calls[0]
	if editedSaved.Request != "<b/>" || editedSaved.Response == nil {
		t.Errorf("request edit changed the saved response: %+v", editedSaved)
	}

	// JSON null clears the saved response.
	if rec := sendJSON(t, mux, http.MethodPut, path, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("clear response: %d %s", rec.Code, rec.Body.String())
	}
	gotProject, err = st.Get(pid)
	if err != nil {
		t.Fatal(err)
	}
	if gotProject.Services[0].Operations[0].Calls[0].Response != nil {
		t.Error("null should clear the saved response")
	}
}
