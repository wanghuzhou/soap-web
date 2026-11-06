package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"soapweb/internal/model"
	"soapweb/internal/store"
)

const remoteWSDLForURLImport = `<?xml version="1.0" encoding="UTF-8"?>
<definitions name="RemoteSvc" targetNamespace="http://example.com/remote"
  xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="http://example.com/remote"
  xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <types>
    <xs:schema targetNamespace="http://example.com/remote" elementFormDefault="qualified">
      <xs:element name="Req"><xs:complexType><xs:sequence>
        <xs:element name="id" type="xs:string"/>
      </xs:sequence></xs:complexType></xs:element>
    </xs:schema>
  </types>
  <message name="M"><part name="p" element="tns:Req"/></message>
  <portType name="PT"><operation name="Lookup"><input message="tns:M"/></operation></portType>
  <binding name="B" type="tns:PT">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Lookup"><soap:operation soapAction="Lookup"/><input><soap:body use="literal"/></input></operation>
  </binding>
  <service name="RemoteSvc"><port name="P" binding="tns:B"><soap:address location="http://example.com/remote"/></port></service>
</definitions>`

func newWSDLImportTestServer(t *testing.T) (*http.ServeMux, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	srv := &Server{Store: st}
	mux := http.NewServeMux()
	srv.Register(mux)
	return mux, st
}

func postWSDLImportJSON(t *testing.T, mux *http.ServeMux, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestFetchWSDLImportsProject covers the full URL import flow: download →
// parse → save, including source metadata and the request template.
func TestFetchWSDLImportsProject(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
		_, _ = w.Write([]byte(remoteWSDLForURLImport))
	}))
	defer remote.Close()

	mux, st := newWSDLImportTestServer(t)
	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]string{
		"url": remote.URL + "/LookupService.wsdl",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Project model.Project `json:"project"`
		URL     string        `json:"url"`
		Service struct {
			Operations []model.Operation `json:"operations"`
		} `json:"service"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Project.ID == "" {
		t.Fatal("project should be saved with an id")
	}
	if resp.Project.Source != "wsdl-url" {
		t.Errorf("Source = %q, want wsdl-url", resp.Project.Source)
	}
	if len(resp.Project.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(resp.Project.Services))
	}
	svc := resp.Project.Services[0]
	if svc.Wsdl != remote.URL+"/LookupService.wsdl" {
		t.Errorf("Wsdl = %q, want source URL", svc.Wsdl)
	}
	if len(svc.Operations) != 1 || svc.Operations[0].Name != "Lookup" {
		t.Fatalf("operations = %+v", svc.Operations)
	}
	if svc.Operations[0].Template == "" {
		t.Error("operation.Template is empty; new request samples would have no skeleton")
	}
	if len(svc.Operations[0].Calls) != 1 {
		t.Fatalf("calls = %d, want 1 generated request sample", len(svc.Operations[0].Calls))
	}
	if svc.Operations[0].Calls[0].Request == "" {
		t.Error("generated request sample is empty; opening the imported project would show a blank editor")
	}
	if svc.Operations[0].Endpoint != "http://example.com/remote" {
		t.Errorf("endpoint = %q", svc.Operations[0].Endpoint)
	}
	if resp.URL != remote.URL+"/LookupService.wsdl" {
		t.Errorf("response url = %q", resp.URL)
	}

	saved, err := st.Get(resp.Project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if saved.Source != "wsdl-url" {
		t.Errorf("persisted Source = %q", saved.Source)
	}
}

// TestParseWSDLAcceptsJSONURL verifies that the original import endpoint also
// accepts a URL payload, so clients treating "parse" as the WSDL entry point
// keep working.
func TestParseWSDLAcceptsJSONURL(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(remoteWSDLForURLImport))
	}))
	defer remote.Close()

	mux, _ := newWSDLImportTestServer(t)
	rec := postWSDLImportJSON(t, mux, "/api/wsdl/parse", map[string]string{"url": remote.URL + "/svc.wsdl"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Project model.Project `json:"project"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Project.Source != "wsdl-url" {
		t.Errorf("Source = %q, want wsdl-url", resp.Project.Source)
	}
}

// TestFetchWSDLRejectsNonWSDL documents the 422 for an HTML login page.
func TestFetchWSDLRejectsNonWSDL(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>login required</body></html>`))
	}))
	defer remote.Close()

	mux, _ := newWSDLImportTestServer(t)
	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]string{"url": remote.URL})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不是 WSDL") {
		t.Errorf("body should explain it is not a WSDL: %s", rec.Body.String())
	}
}

// TestFetchWSDLRejectsBadScheme rejects local-file and other schemes.
func TestFetchWSDLRejectsBadScheme(t *testing.T) {
	mux, _ := newWSDLImportTestServer(t)
	for _, raw := range []string{"file:///C:/secret.wsdl", "ftp://host/x.wsdl"} {
		rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]string{"url": raw})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body=%s)", raw, rec.Code, rec.Body.String())
		}
	}
}

// TestFetchWSDLMissingURL rejects an empty payload.
func TestFetchWSDLMissingURL(t *testing.T) {
	mux, _ := newWSDLImportTestServer(t)
	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestFetchWSDLBadJSON rejects a malformed body.
func TestFetchWSDLBadJSON(t *testing.T) {
	mux, _ := newWSDLImportTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/wsdl/fetch", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestFetchWSDLUsesProjectProxy verifies that a project's proxy is honoured.
func TestFetchWSDLUsesProjectProxy(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(remoteWSDLForURLImport))
	}))
	defer proxySrv.Close()

	mux, st := newWSDLImportTestServer(t)
	pu, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.AddProject(model.Project{
		Name:  "P",
		Proxy: &model.ProxySettings{Mode: "custom", Type: "http", Host: pu.Hostname(), Port: portOf(pu)},
	})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]string{
		"url":       "http://target.invalid/svc.wsdl",
		"projectId": proj.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestFetchWSDLExplicitCustomProxy verifies the dialog's custom proxy option:
// the request carries proxy settings directly instead of a project id.
func TestFetchWSDLExplicitCustomProxy(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(remoteWSDLForURLImport))
	}))
	defer proxySrv.Close()

	mux, _ := newWSDLImportTestServer(t)
	pu, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]any{
		"url": "http://target.invalid/svc.wsdl",
		"proxy": map[string]any{
			"mode": "custom", "type": "http",
			"host": pu.Hostname(), "port": portOf(pu),
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestFetchWSDLRejectsInvalidProxy validates proxy settings before fetching.
func TestFetchWSDLRejectsInvalidProxy(t *testing.T) {
	mux, _ := newWSDLImportTestServer(t)
	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]any{
		"url":   "http://target.invalid/svc.wsdl",
		"proxy": map[string]any{"mode": "custom", "type": "http", "host": "", "port": 0},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestParseWSDLFileStillWorks guards the upload path against the refactor.
func TestParseWSDLFileStillWorks(t *testing.T) {
	mux, _ := newWSDLImportTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/wsdl/parse", strings.NewReader(remoteWSDLForURLImport))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Project model.Project `json:"project"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Project.Source != "wsdl" {
		t.Errorf("Source = %q, want wsdl", resp.Project.Source)
	}
}
