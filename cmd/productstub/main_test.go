package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// captureLog installs a buffer as the stdlib logger's output for one test and
// returns it once the test ends. The middleware logs through the package-level
// logger, which is the same one main() leaves at its default, so this is what a
// hand-run process prints.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// The line has to name the path and the status. Without the status, "did the
// stand-in answer 200 or refuse with 401" - the distinction the whole sign-in
// path rests on - is invisible, and the log would not answer the question it
// exists for.
func TestTheLogLineNamesThePathAndTheStatus(t *testing.T) {
	buf := captureLog(t)
	h := withRequestLog(clienttest.New().Handler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/sms/send",
		strings.NewReader(`{"phone":"13800001234"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("sms/send = %d, want 200 (the fixture phone is accepted)", rec.Code)
	}
	got := buf.String()
	if !strings.Contains(got, "POST /v1/auth/sms/send") {
		t.Fatalf("log = %q, want it to name the method and path", got)
	}
	if !strings.Contains(got, "-> 200") {
		t.Fatalf("log = %q, want it to report the status", got)
	}
}

// The completions line is marked, because it is the only one that proves a model
// turn crossed the platform boundary rather than reaching a third-party
// endpoint (需求基线 B4, and what V-35 violated). A log where that line looks
// like every other line leaves the reader to know the paths by heart.
func TestTheCompletionsLineIsMarked(t *testing.T) {
	buf := captureLog(t)
	h := withRequestLog(clienttest.New().Handler())

	rec := httptest.NewRecorder()
	// No bearer: the stand-in refuses, which is also the status this pins.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"buding-privacy-1"}`)))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("completions without a token = %d, want 401", rec.Code)
	}
	got := buf.String()
	if !strings.Contains(got, "POST /v1/chat/completions") || !strings.Contains(got, "-> 401") {
		t.Fatalf("log = %q, want path and status", got)
	}
	if !strings.Contains(got, "model turn through the platform boundary") {
		t.Fatalf("log = %q, want the completions line marked", got)
	}
}

// Non-completions lines must NOT carry the marker, or the marker stops meaning
// anything: an auth call and a model turn would read alike.
func TestOrdinaryLinesAreNotMarked(t *testing.T) {
	buf := captureLog(t)
	h := withRequestLog(clienttest.New().Handler())

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/client/bootstrap", nil))

	if strings.Contains(buf.String(), "platform boundary") {
		t.Fatalf("log = %q, want no marker on a non-completions path", buf.String())
	}
}
