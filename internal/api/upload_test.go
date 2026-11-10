package api

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReadUploadLimitedRejectsOversizedBody pins the hard cap: the raw-body
// path must fail instead of returning a silently truncated buffer.
func TestReadUploadLimitedRejectsOversizedBody(t *testing.T) {
	const limit = 16
	req := httptest.NewRequest(http.MethodPost, "/api/wsdl/parse",
		strings.NewReader(strings.Repeat("x", limit+1)))
	rec := httptest.NewRecorder()

	_, _, err := readUploadLimited(rec, req, limit)
	if err == nil {
		t.Fatal("oversized body was accepted")
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want *http.MaxBytesError", err)
	}
}

// TestReadUploadLimitedRejectsOversizedMultipart covers the multipart branch:
// ParseMultipartForm's argument is only the memory threshold, so the total cap
// has to come from MaxBytesReader.
func TestReadUploadLimitedRejectsOversizedMultipart(t *testing.T) {
	const limit = 1024
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "big.wsdl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), limit+1)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/wsdl/parse", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	_, _, err = readUploadLimited(rec, req, limit)
	if err == nil {
		t.Fatal("oversized multipart upload was accepted")
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want *http.MaxBytesError", err)
	}
}

// TestWriteUploadErrMapsStatus checks the client-visible mapping: a deliberate
// size limit is 413, every other upload failure stays a 400.
func TestWriteUploadErrMapsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUploadErr(rec, &http.MaxBytesError{Limit: maxUploadBytes})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too-large status = %d, want 413", rec.Code)
	}

	rec = httptest.NewRecorder()
	writeUploadErr(rec, errors.New("boom"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("generic status = %d, want 400", rec.Code)
	}
}
