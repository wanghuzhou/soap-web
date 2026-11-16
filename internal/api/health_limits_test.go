package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"soapweb/internal/model"
)

// TestHealthReportsTimeoutLimits pins the contract the editor relies on: the
// server publishes the bounds it actually enforces, so the frontend does not
// have to keep a hand-copied duplicate of them.
//
// The duplicate is what drifted before: web/assets/js/constants.js declared
// 1000 / 60000 / 600000 independently of model.MinTimeoutMS and friends, so
// changing one side made the editor accept a value the server then silently
// rewrote.
func TestHealthReportsTimeoutLimits(t *testing.T) {
	_, mux, _ := newTestServer(t)

	body, rec := getRaw(t, mux, http.MethodGet, "/api/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, body)
	}

	var payload struct {
		Status string `json:"status"`
		Limits struct {
			MinTimeoutMS     int `json:"minTimeoutMs"`
			DefaultTimeoutMS int `json:"defaultTimeoutMs"`
			MaxTimeoutMS     int `json:"maxTimeoutMs"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}

	if payload.Limits.MinTimeoutMS != model.MinTimeoutMS {
		t.Errorf("minTimeoutMs = %d, want %d", payload.Limits.MinTimeoutMS, model.MinTimeoutMS)
	}
	if payload.Limits.DefaultTimeoutMS != model.DefaultTimeoutMS {
		t.Errorf("defaultTimeoutMs = %d, want %d", payload.Limits.DefaultTimeoutMS, model.DefaultTimeoutMS)
	}
	if payload.Limits.MaxTimeoutMS != model.MaxTimeoutMS {
		t.Errorf("maxTimeoutMs = %d, want %d", payload.Limits.MaxTimeoutMS, model.MaxTimeoutMS)
	}
}
