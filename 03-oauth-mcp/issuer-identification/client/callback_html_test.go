//go:build !windows

package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The callback message can embed untrusted query parameters from the
// authorization response (e.g. the `error` value), so it must be escaped
// before being interpolated into the callback HTML page.
func TestWriteCallbackHTMLEscapesMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := `<script>alert('xss')</script>`
	writeCallbackHTML(rec, "Authentication failed: "+payload)

	body := rec.Body.String()
	if strings.Contains(body, payload) {
		t.Fatalf("callback HTML contains unescaped payload: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("callback HTML missing escaped payload: %s", body)
	}
}
