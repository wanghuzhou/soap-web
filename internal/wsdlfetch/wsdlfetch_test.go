package wsdlfetch

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"soapweb/internal/model"
	"soapweb/internal/wsdl"
)

// minimalWSDL is a valid WSDL 1.1 document with a single operation, used to
// exercise the fetch → parse path. __NAME__ is replaced per test.
const minimalWSDL = `<?xml version="1.0" encoding="UTF-8"?>
<definitions name="__NAME__" targetNamespace="http://example.com/svc"
  xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="http://example.com/svc"
  xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <types>
    <xs:schema targetNamespace="http://example.com/svc" elementFormDefault="qualified">
      <xs:element name="Req"><xs:complexType><xs:sequence>
        <xs:element name="id" type="xs:string"/>
      </xs:sequence></xs:complexType></xs:element>
    </xs:schema>
  </types>
  <message name="M"><part name="p" element="tns:Req"/></message>
  <portType name="PT"><operation name="Query"><input message="tns:M"/></operation></portType>
  <binding name="B" type="tns:PT">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Query"><soap:operation soapAction="Query"/><input><soap:body use="literal"/></input></operation>
  </binding>
  <service name="Svc"><port name="P" binding="tns:B"><soap:address location="http://example.com/svc"/></port></service>
</definitions>`

func wsdlWithName(name string) string {
	return strings.ReplaceAll(minimalWSDL, "__NAME__", name)
}

// TestFetchHappyPath covers a normal download and the metadata derived from
// the request URL.
func TestFetchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got == "" {
			t.Errorf("Accept header should be set")
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "soap-web") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
		_, _ = w.Write([]byte(wsdlWithName("RemoteSvc")))
	}))
	defer srv.Close()

	res, err := Fetch(srv.URL+"/service.wsdl", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Data) == 0 {
		t.Fatal("Data is empty")
	}
	if res.Name != "service" {
		t.Errorf("Name = %q, want service (suffix removed)", res.Name)
	}
	if res.FinalURL != srv.URL+"/service.wsdl" {
		t.Errorf("FinalURL = %q", res.FinalURL)
	}
}

// TestFetchFollowsRedirect verifies that FinalURL records the post-redirect
// address, not the address the user typed.
func TestFetchFollowsRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/final/Calculator.wsdl", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(wsdlWithName("Calc")))
	})
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final/Calculator.wsdl", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := Fetch(srv.URL+"/start", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.FinalURL != srv.URL+"/final/Calculator.wsdl" {
		t.Errorf("FinalURL = %q, want redirected URL", res.FinalURL)
	}
	if res.Name != "Calculator" {
		t.Errorf("Name = %q, want Calculator", res.Name)
	}
}

// TestFetchRejectsInvalidURL covers scheme and shape validation.
func TestFetchRejectsInvalidURL(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"not a url",
		"file:///C:/secret.wsdl",
		"ftp://host/a.wsdl",
		"gopher://host/a",
		"data:text/xml,<definitions/>",
		"javascript:alert(1)",
		"http://",
	}
	for _, raw := range cases {
		if _, err := Fetch(raw, nil); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("Fetch(%q) error = %v, want ErrInvalidURL", raw, err)
		}
	}
}

// TestFetchRejectsNonWSDL ensures an HTML login page or arbitrary XML is
// reported as "not a WSDL", not as a confusing parse failure.
func TestFetchRejectsNonWSDL(t *testing.T) {
	bodies := []string{
		`<!DOCTYPE html><html><body>login required</body></html>`,
		`<html><body><p>definitions are in another castle</p></body></html>`,
		`{"error":"not found"}`,
		`<root><definitions/></root>`,
	}
	for _, body := range bodies {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := Fetch(srv.URL, nil)
		srv.Close()
		if !errors.Is(err, ErrNotWSDL) {
			t.Errorf("Fetch body %q error = %v, want ErrNotWSDL", body, err)
		}
	}
}

// TestFetchReportsHTTPError verifies non-200 responses carry the status code.
func TestFetchReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Fetch(srv.URL, nil)
	if err == nil {
		t.Fatal("404 should be an error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404: %v", err)
	}
}

// TestFetchCapsSize rejects an oversized document before reading it all.
func TestFetchCapsSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Header().Set("Content-Length", strconv.Itoa(MaxBytes+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := Fetch(srv.URL, nil)
	if err == nil {
		t.Fatal("oversized WSDL should fail")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("error should mention the size limit: %v", err)
	}
}

// TestFetchEmptyBody rejects a 200 response with no content.
func TestFetchEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := Fetch(srv.URL, nil); err == nil {
		t.Fatal("empty response should fail")
	}
}

// TestFetchGBKAndParse is a regression test for double-decoding: the fetched
// body must be UTF-8 with a rewritten declaration, and wsdl.Parse must produce
// a correct Chinese service name from it.
func TestFetchGBKAndParse(t *testing.T) {
	gbkName, err := simplifiedchinese.GBK.NewEncoder().String("查询服务")
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte(strings.ReplaceAll(minimalWSDL, "__NAME__", gbkName))
	doc = []byte(strings.Replace(string(doc), `encoding="UTF-8"`, `encoding="GBK"`, 1))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=GBK")
		_, _ = w.Write(doc)
	}))
	defer srv.Close()

	res, err := Fetch(srv.URL+"/x.wsdl", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(string(res.Data), "查询服务") {
		t.Errorf("Data is not UTF-8: %q", res.Data)
	}
	if !strings.Contains(string(res.Data), `encoding="UTF-8"`) {
		t.Errorf("XML declaration should be rewritten to UTF-8: %q", res.Data[:80])
	}
	service, err := wsdl.Parse(res.Data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if service.Name != "查询服务" {
		t.Errorf("service.Name = %q, want 查询服务", service.Name)
	}
}

// TestFetchGBKFromContentType covers a legacy charset announced only by the
// HTTP header (no encoding attribute in the XML declaration).
func TestFetchGBKFromContentType(t *testing.T) {
	gbkName, err := simplifiedchinese.GBK.NewEncoder().String("查询服务")
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte(strings.ReplaceAll(minimalWSDL, "__NAME__", gbkName))
	doc = []byte(strings.Replace(string(doc), ` encoding="UTF-8"`, ``, 1))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=GBK")
		_, _ = w.Write(doc)
	}))
	defer srv.Close()

	res, err := Fetch(srv.URL+"/x.wsdl", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(string(res.Data), "查询服务") {
		t.Errorf("Data is not UTF-8: %q", res.Data)
	}
	service, err := wsdl.Parse(res.Data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if service.Name != "查询服务" {
		t.Errorf("service.Name = %q, want 查询服务", service.Name)
	}
}

// TestFetchUsesProxy confirms the project proxy settings are honoured.
func TestFetchUsesProxy(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "" {
			t.Errorf("proxy should receive an absolute URI, got %v", r.URL)
		}
		_, _ = w.Write([]byte(wsdlWithName("Proxied")))
	}))
	defer proxySrv.Close()

	pu, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(pu.Port())

	res, err := Fetch("http://target.invalid/svc.wsdl", &model.ProxySettings{
		Mode: "custom", Type: "http", Host: pu.Hostname(), Port: port,
	})
	if err != nil {
		t.Fatalf("Fetch through proxy: %v", err)
	}
	if len(res.Data) == 0 {
		t.Error("expected data through proxy")
	}
}

// TestFetchInvalidProxy reports a bad proxy configuration rather than trying
// the request with a broken client.
func TestFetchInvalidProxy(t *testing.T) {
	_, err := Fetch("http://127.0.0.1:1/x.wsdl", &model.ProxySettings{
		Mode: "custom", Type: "http", Host: "", Port: 0,
	})
	if err == nil {
		t.Fatal("invalid proxy should fail")
	}
	if !strings.Contains(err.Error(), "代理") {
		t.Errorf("error should mention proxy: %v", err)
	}
}

func TestSuggestName(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"http://h/service.wsdl", "service"},
		{"http://h/a/b/Calculator.XML", "Calculator"},
		{"http://example.com/", "example.com"},
		{"http://svc.local/services/ws/api?wsdl", "api"},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := suggestName(u); got != c.want {
			t.Errorf("suggestName(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestLooksLikeWSDL(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`<definitions/>`, true},
		{`<?xml version="1.0"?><definitions/>`, true},
		{"\xef\xbb\xbf<definitions/>", true},
		{`<wsdl:definitions xmlns:wsdl="..."/>`, true},
		{"  \n<!-- comment -->\n<wsdl:definitions/>", true},
		{`<!DOCTYPE html><html></html>`, false},
		{`<html><body>definitions</body></html>`, false},
		{`{"error":"x"}`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := looksLikeWSDL([]byte(c.body)); got != c.want {
			t.Errorf("looksLikeWSDL(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}
