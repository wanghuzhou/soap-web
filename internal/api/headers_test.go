package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"soapweb/internal/model"
)

// sendJSON posts a JSON body to path and decodes the response into out.
func sendJSON(t *testing.T, mux *http.ServeMux, method, path string, body any, out any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s: %v (%s)", method, path, err, rec.Body.String())
		}
	}
	return rec
}

// TestSendForwardsCustomHeaders proves the header a user configured actually
// reaches the target server.
func TestSendForwardsCustomHeaders(t *testing.T) {
	_, mux, _ := newTestServer(t)

	var got http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte("<ok/>"))
	}))
	defer target.Close()

	rec := sendJSON(t, mux, http.MethodPost, "/api/send", model.SendRequest{
		URL:     target.URL,
		Method:  "POST",
		Headers: map[string]string{"Authorization": "Bearer abc", "X-Trace": "42"},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got.Get("Authorization") != "Bearer abc" {
		t.Errorf("Authorization = %q, want Bearer abc", got.Get("Authorization"))
	}
	if got.Get("X-Trace") != "42" {
		t.Errorf("X-Trace = %q, want 42", got.Get("X-Trace"))
	}
}

// TestSendRejectsBadHeaderName turns an invalid name into a 400 with a message
// instead of an opaque transport error.
func TestSendRejectsBadHeaderName(t *testing.T) {
	_, mux, _ := newTestServer(t)

	rec := sendJSON(t, mux, http.MethodPost, "/api/send", model.SendRequest{
		URL:     "http://127.0.0.1:1/x",
		Headers: map[string]string{"Bad Header": "v"},
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("非法字符")) {
		t.Errorf("error does not name the problem: %s", rec.Body.String())
	}
}

// TestSendClampsTimeout keeps an out-of-range timeout from being rejected.
func TestSendClampsTimeout(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, model.DefaultTimeoutMS},
		{1, model.MinTimeoutMS},
		{model.MaxTimeoutMS + 1, model.MaxTimeoutMS},
	}
	for _, c := range cases {
		if got := clampTimeout(c.in); got != c.want {
			t.Errorf("clampTimeout(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestProjectHeadersPersistAndValidate covers the project-level headers: a
// valid set survives a write/read round trip, an invalid one is rejected.
func TestProjectHeadersPersistAndValidate(t *testing.T) {
	_, mux, _ := newTestServer(t)

	var project model.Project
	rec := sendJSON(t, mux, http.MethodPost, "/api/projects", model.Project{
		Name:    "p",
		Headers: []model.Header{{Name: "Authorization", Value: "Bearer x"}},
	}, &project)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	var loaded model.Project
	if rec := sendJSON(t, mux, http.MethodGet, "/api/projects/"+project.ID, nil, &loaded); rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	if len(loaded.Headers) != 1 || loaded.Headers[0].Value != "Bearer x" {
		t.Errorf("headers lost: %+v", loaded.Headers)
	}

	loaded.Headers = append(loaded.Headers, model.Header{Name: "Bad Name", Value: "v"})
	if rec := sendJSON(t, mux, http.MethodPut, "/api/projects/"+project.ID, loaded, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid header accepted: %d %s", rec.Code, rec.Body.String())
	}
}

// TestCallHeadersAndTimeoutPersist covers the per-sample overrides.
func TestCallHeadersAndTimeoutPersist(t *testing.T) {
	_, mux, _ := newTestServer(t)

	var project model.Project
	sendJSON(t, mux, http.MethodPost, "/api/projects", model.Project{Name: "p"}, &project)
	base := "/api/projects/" + project.ID
	var svc model.Service
	sendJSON(t, mux, http.MethodPost, base+"/services", model.Service{Name: "s"}, &svc)
	var op model.Operation
	sendJSON(t, mux, http.MethodPost, base+"/services/"+svc.ID+"/operations", model.Operation{Name: "o"}, &op)
	callsPath := base + "/services/" + svc.ID + "/operations/" + op.ID + "/calls"

	var call model.Call
	rec := sendJSON(t, mux, http.MethodPost, callsPath, model.Call{
		Title:     "c",
		Headers:   []model.Header{{Name: "X-Trace", Value: "1"}},
		TimeoutMS: 120000,
	}, &call)
	if rec.Code != http.StatusOK {
		t.Fatalf("add call: %d %s", rec.Code, rec.Body.String())
	}
	if call.TimeoutMS != 120000 || len(call.Headers) != 1 {
		t.Errorf("call not saved as sent: %+v", call)
	}

	// An invalid header on an update is rejected.
	call.Headers = []model.Header{{Name: "Bad:Name", Value: "v"}}
	if rec := sendJSON(t, mux, http.MethodPut, callsPath+"/"+call.ID, call, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid call header accepted: %d %s", rec.Code, rec.Body.String())
	}
}

// TestDuplicateCallEndpoint covers the new route end to end.
func TestDuplicateCallEndpoint(t *testing.T) {
	_, mux, _ := newTestServer(t)

	var project model.Project
	sendJSON(t, mux, http.MethodPost, "/api/projects", model.Project{Name: "p"}, &project)
	base := "/api/projects/" + project.ID
	var svc model.Service
	sendJSON(t, mux, http.MethodPost, base+"/services", model.Service{Name: "s"}, &svc)
	var op model.Operation
	sendJSON(t, mux, http.MethodPost, base+"/services/"+svc.ID+"/operations", model.Operation{Name: "o"}, &op)
	callsPath := base + "/services/" + svc.ID + "/operations/" + op.ID + "/calls"

	var call model.Call
	sendJSON(t, mux, http.MethodPost, callsPath, model.Call{Title: "查询", Request: "<q/>"}, &call)

	var copied model.Call
	rec := sendJSON(t, mux, http.MethodPost, callsPath+"/"+call.ID+"/duplicate", nil, &copied)
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body.String())
	}
	if copied.ID == call.ID || copied.Title != "查询 (副本)" || copied.Request != "<q/>" {
		t.Errorf("copied = %+v", copied)
	}

	// Duplicating a call that does not exist is a 404, not a 500.
	if rec := sendJSON(t, mux, http.MethodPost, callsPath+"/missing/duplicate", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing call duplicate status = %d, want 404", rec.Code)
	}
}
