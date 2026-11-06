package transport

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"soapweb/internal/model"
)

// TestSendDirectIgnoresEnvironmentProxy verifies that "direct" mode does not
// use the environment proxy (which would make the request fail).
func TestSendDirectIgnoresEnvironmentProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct-ok"))
	}))
	defer srv.Close()

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")

	resp := Send(model.SendRequest{URL: srv.URL, Method: "GET", TimeoutMS: 5000}, &model.ProxySettings{Mode: "direct"})
	if resp.Error != "" {
		t.Fatalf("direct mode should ignore env proxy: %s", resp.Error)
	}
	if resp.Body != "direct-ok" {
		t.Errorf("body = %q", resp.Body)
	}
}

// TestSendCustomHTTPProxy verifies traffic goes through the configured HTTP
// proxy. The target host does not resolve, so success proves the proxy was used.
func TestSendCustomHTTPProxy(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An HTTP proxy receives the absolute URI in r.URL.
		if r.URL.Host == "" {
			t.Errorf("proxy did not receive absolute URI: %v", r.URL)
		}
		if user, pass, ok := basicAuth(r); ok {
			if user != "u" || pass != "p" {
				t.Errorf("credentials = %q/%q", user, pass)
			}
		}
		_, _ = w.Write([]byte("via-proxy"))
	}))
	defer proxySrv.Close()

	pu, _ := url.Parse(proxySrv.URL)
	host := pu.Hostname()
	port := 0
	if p := pu.Port(); p != "" {
		port = atoi(p)
	}

	resp := Send(model.SendRequest{
		URL:       "http://target.invalid/svc",
		Method:    "GET",
		TimeoutMS: 5000,
	}, &model.ProxySettings{Mode: "custom", Type: "http", Host: host, Port: port, Username: "u", Password: "p"})
	if resp.Error != "" {
		t.Fatalf("request via proxy failed: %s", resp.Error)
	}
	if resp.Body != "via-proxy" {
		t.Errorf("body = %q", resp.Body)
	}
}

// TestSendCustomHTTPProxyNoAuth verifies optional credentials (empty allowed).
func TestSendCustomHTTPProxyNoAuth(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := basicAuth(r); ok {
			t.Errorf("unexpected credentials")
		}
		_, _ = w.Write([]byte("no-auth"))
	}))
	defer proxySrv.Close()

	pu, _ := url.Parse(proxySrv.URL)
	resp := Send(model.SendRequest{
		URL:       "http://target.invalid/x",
		Method:    "GET",
		TimeoutMS: 5000,
	}, &model.ProxySettings{Mode: "custom", Type: "http", Host: pu.Hostname(), Port: atoi(pu.Port())})
	if resp.Error != "" {
		t.Fatalf("failed: %s", resp.Error)
	}
	if resp.Body != "no-auth" {
		t.Errorf("body = %q", resp.Body)
	}
}

func TestInvalidProxyConfigRejected(t *testing.T) {
	cases := []*model.ProxySettings{
		{Mode: "custom", Type: "http", Host: "", Port: 8080},
		{Mode: "custom", Type: "http", Host: "127.0.0.1", Port: 0},
		{Mode: "custom", Type: "ftp", Host: "127.0.0.1", Port: 8080},
		{Mode: "bogus"},
	}
	for i, c := range cases {
		resp := Send(model.SendRequest{URL: "http://127.0.0.1:1/"}, c)
		if resp.Error == "" {
			t.Errorf("case %d: expected validation error", i)
		}
		if !strings.Contains(resp.Error, "代理") {
			t.Errorf("case %d: error = %q", i, resp.Error)
		}
	}
}

func basicAuth(r *http.Request) (string, string, bool) {
	// Proxy-Authorization is used for HTTP proxies.
	h := r.Header.Get("Proxy-Authorization")
	if h == "" {
		return "", "", false
	}
	const p = "Basic "
	if !strings.HasPrefix(h, p) {
		return "", "", false
	}
	// Minimal base64 decode via http.Request helper.
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", h)
	return r2.BasicAuth()
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
