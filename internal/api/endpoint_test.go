package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"soapweb/internal/model"
)

// TestSendRejectsNonHTTPEndpoint is the API-level half of the scheme check:
// /api/send must answer 400 for a URL it will never be able to send, instead of
// passing it to the transport and returning 200 with a nested transport error.
func TestSendRejectsNonHTTPEndpoint(t *testing.T) {
	_, mux, _ := newTestServer(t)

	for _, u := range []string{
		"file:///C:/Windows/win.ini",
		"ftp://example.com/x",
		"gopher://example.com/",
		"data:text/plain,hi",
		"http://",      // no host
		"http:///path", // no host
	} {
		rec := sendJSON(t, mux, http.MethodPost, "/api/send", model.SendRequest{
			URL: u, Method: "POST",
		}, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s -> status %d, want 400 (%s)", u, rec.Code, rec.Body.String())
		}
	}
}

// TestSendAcceptsHTTPAndHTTPS guards against the check being too strict.
func TestSendAcceptsHTTPAndHTTPS(t *testing.T) {
	_, mux, _ := newTestServer(t)

	var got int
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got++
		_, _ = w.Write([]byte("<ok/>"))
	}))
	target.StartTLS()
	defer target.Close()

	// https with a self-signed cert fails TLS, but it must be a *transport*
	// error (200 + error field), not a 400 from validation.
	rec := sendJSON(t, mux, http.MethodPost, "/api/send", model.SendRequest{
		URL: target.URL, Method: "POST",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("https endpoint was rejected by validation: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSendHeaderCaseDoesNotFlipValue checks that two spellings of one header
// reaching /api/send resolve the same way over repeated calls; the transport
// test covers the ordering itself, this one proves the API does not add its own
// nondeterminism on the way through JSON.
func TestSendHeaderCaseDoesNotFlipValue(t *testing.T) {
	_, mux, _ := newTestServer(t)

	var seen []string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	for i := 0; i < 20; i++ {
		rec := sendJSON(t, mux, http.MethodPost, "/api/send", model.SendRequest{
			URL:    target.URL,
			Method: "POST",
			Headers: map[string]string{
				"content-type": "text/plain",
				"Content-Type": "application/soap+xml",
			},
		}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	}
	if len(seen) == 0 {
		t.Fatal("target received no requests")
	}
	for i, v := range seen {
		if v != seen[0] {
			t.Fatalf("request %d sent Content-Type %q, request 0 sent %q", i, v, seen[0])
		}
	}
	if seen[0] != "application/soap+xml" {
		t.Errorf("Content-Type = %q, want the canonical spelling to win", seen[0])
	}
}
