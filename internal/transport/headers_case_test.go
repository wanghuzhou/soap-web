package transport

import (
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sync"
	"testing"

	"soapweb/internal/model"
)

// recordServer captures the headers of every request it receives.
type recordServer struct {
	*httptest.Server
	mu   sync.Mutex
	seen []http.Header
}

func newRecordServer(t *testing.T) *recordServer {
	t.Helper()
	rs := &recordServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.seen = append(rs.seen, r.Header.Clone())
		rs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordServer) headers() []http.Header {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]http.Header(nil), rs.seen...)
}

// TestHeaderCaseVariantsResolveDeterministically is the regression test for the
// nondeterministic header precedence.
//
// The frontend builds the header map as a plain JS object, where "content-type"
// and "Content-Type" are different keys, so both reach the server. http.Header.Set
// canonicalises the name, putting both in one slot, and ranging over the map made
// the winner depend on Go's randomised iteration order: the same request sent 30
// times produced 27/3 splits between the two values. It must now be fixed.
func TestHeaderCaseVariantsResolveDeterministically(t *testing.T) {
	srv := newRecordServer(t)

	const runs = 40
	for i := 0; i < runs; i++ {
		Send(model.SendRequest{
			URL:    srv.URL,
			Method: "POST",
			Headers: map[string]string{
				"content-type": "text/plain",
				"Content-Type": "application/soap+xml",
			},
			Body: "<x/>",
		}, nil)
	}

	seen := srv.headers()
	if len(seen) != runs {
		t.Fatalf("server saw %d requests, want %d", len(seen), runs)
	}
	first := seen[0].Get("Content-Type")
	for i, h := range seen {
		if got := h.Get("Content-Type"); got != first {
			t.Fatalf("request %d Content-Type = %q but request 0 sent %q: "+
				"the winner still depends on map iteration order", i, got, first)
		}
	}
	// The canonical spelling is authoritative, matching what the editor shows:
	// Content-Type is driven by the Content-Type / SOAPAction controls.
	if first != "application/soap+xml" {
		t.Errorf("Content-Type = %q, want the canonical spelling to win", first)
	}
}

// TestHeaderCaseVariantsReversedInputOrder checks the outcome does not depend on
// which spelling happens to be inserted first either.
func TestHeaderCaseVariantsReversedInputOrder(t *testing.T) {
	srv := newRecordServer(t)
	for i := 0; i < 20; i++ {
		Send(model.SendRequest{
			URL:    srv.URL,
			Method: "POST",
			Headers: map[string]string{
				"SOAPACTION": `"urn:b"`,
				"soapaction": `"urn:a"`,
			},
		}, nil)
	}
	seen := srv.headers()
	first := seen[0].Get("SOAPAction")
	for i, h := range seen {
		if got := h.Get("SOAPAction"); got != first {
			t.Fatalf("request %d SOAPAction = %q, want %q", i, got, first)
		}
	}
	if first != `"urn:b"` {
		t.Errorf("SOAPAction = %q, want the canonical spelling to win", first)
	}
}

// TestDistinctHeadersAllReachTheServer makes sure sorting did not drop or merge
// unrelated headers.
func TestDistinctHeadersAllReachTheServer(t *testing.T) {
	srv := newRecordServer(t)
	want := map[string]string{
		"Authorization": "Bearer t",
		"X-App-Key":     "k",
		"X-Trace-Id":    "123",
		"Accept":        "text/xml",
	}
	headers := map[string]string{}
	for k, v := range want {
		headers[k] = v
	}
	headers["accept"] = "application/xml" // collides with Accept (case only)

	if r := Send(model.SendRequest{URL: srv.URL, Method: "POST", Headers: headers}, nil); r.Error != "" {
		t.Fatalf("Send: %v", r.Error)
	}
	got := srv.headers()[0]
	for k, v := range want {
		if k == "Accept" {
			continue // the colliding pair is covered above
		}
		if got.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, got.Get(k), v)
		}
	}
}

// TestNonHTTPEndpointsRejected is the regression test for /api/send accepting any
// scheme: the error used to surface from client.Do as an opaque
// "unsupported protocol scheme" after the request was already built.
func TestNonHTTPEndpointsRejected(t *testing.T) {
	for _, u := range []string{
		"file:///C:/Windows/win.ini",
		"ftp://example.com/x",
		"gopher://example.com/",
		"data:text/plain,hi",
	} {
		r := Send(model.SendRequest{URL: u, Method: "POST"}, nil)
		if r.Error == "" {
			t.Errorf("%s was accepted", u)
			continue
		}
		// The message must name the problem, not leak a transport-level detail.
		if !contains(r.Error, "http/https") {
			t.Errorf("%s -> %q, want a message about http/https", u, r.Error)
		}
	}
}

// TestHostlessEndpointRejected covers http URLs with no host.
func TestHostlessEndpointRejected(t *testing.T) {
	for _, u := range []string{"http://", "http:///path"} {
		if r := Send(model.SendRequest{URL: u}, nil); r.Error == "" {
			t.Errorf("%q was accepted", u)
		}
	}
}

// TestValidEndpointsStillWork guards against the validation being too strict.
func TestValidEndpointsStillWork(t *testing.T) {
	srv := newRecordServer(t)
	for _, u := range []string{srv.URL, "HTTP://" + hostOnly(srv.URL) + "/"} {
		if r := Send(model.SendRequest{URL: u, Method: "POST"}, nil); r.Error != "" {
			t.Errorf("%s rejected: %s", u, r.Error)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hostOnly(raw string) string {
	// raw looks like "http://127.0.0.1:PORT"
	const p = "http://"
	if len(raw) > len(p) {
		return raw[len(p):]
	}
	return raw
}

// TestApplyHeadersCanonicalises is a direct unit test of the helper.
func TestApplyHeadersCanonicalises(t *testing.T) {
	dst := http.Header{}
	applyHeaders(dst, map[string]string{
		"x-lower":      "1",
		"X-UPPER":      "2",
		"Content-Type": "text/xml",
		"content-type": "text/plain",
	})
	if got := dst.Get("X-Lower"); got != "1" {
		t.Errorf("x-lower = %q", got)
	}
	if got := dst.Get("X-Upper"); got != "2" {
		t.Errorf("X-UPPER = %q", got)
	}
	if got := dst.Get("Content-Type"); got != "text/xml" {
		t.Errorf("Content-Type = %q, want the canonical spelling to win", got)
	}
	if got := dst[textproto.CanonicalMIMEHeaderKey("content-type")]; len(got) != 1 {
		t.Errorf("Content-Type has %d values, want exactly 1: %v", len(got), got)
	}
}
