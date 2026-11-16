package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestFetchWSDLMissingProjectIsRejected is the regression test for the silent
// proxy downgrade.
//
// When projectId named a project that no longer existed (another tab deleted
// it), the handler ignored the lookup error and fell through to a direct
// connection. On an intranet that looks like "明明配了代理还是连不上", with
// nothing in the UI or the log saying the chosen proxy was never used.
//
// The happy path — a named project whose proxy really is used — is already
// covered by TestFetchWSDLUsesProjectProxy.
func TestFetchWSDLMissingProjectIsRejected(t *testing.T) {
	mux, _ := newWSDLImportTestServer(t)

	rec := postWSDLImportJSON(t, mux, "/api/wsdl/fetch", map[string]any{
		"url":       "http://127.0.0.1:1/does-not-exist?wsdl",
		"projectId": "no-such-project",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "工程不存在") {
		t.Errorf("message should say the project is gone: %s", body)
	}
	// It must fail before touching the network: a direct attempt would surface
	// as a connection error instead of this message.
	if strings.Contains(body, "获取 WSDL 失败") {
		t.Errorf("handler attempted the fetch anyway: %s", body)
	}
}

