//go:build cgo

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOpenCodeExhaustedQuotaDoesNotFallback(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"type":"FreeUsageLimitError","message":"Free usage limit reached. Subscribe to OpenCode Go to keep going."}}`)
	}))
	defer ts.Close()
	t.Setenv("OPENCODE_BASE_URL", ts.URL)

	payload := []byte(`{"model":"mimo-v2.6-flash-free","messages":[{"role":"user","content":"Reply OK."}]}`)
	raw, err := json.Marshal(executorRequest{Model: "mimo-v2.6-flash-free", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	response, err := execute(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result envelope
	if err := json.Unmarshal(response, &result); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("exhausted quota made %d upstream requests; want 1", got)
	}
	if result.Error == nil || result.Error.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("upstream 429 was not preserved: %s", response)
	}
}

func TestOpenCodeSidecarUsesOpenAIRoute(t *testing.T) {
	var requestedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()
	t.Setenv("OPENCODE_BASE_URL", ts.URL)
	response, err := callOpenCode("mimo-v2.6-flash-free", []byte(`{"model":"mimo-v2.6-flash-free","messages":[]}`), true)
	if err != nil {
		t.Fatalf("sidecar request used %q: %v", requestedPath, err)
	}
	defer response.body.Close()
	if requestedPath != "/v1/chat/completions" {
		t.Fatalf("unexpected sidecar route %q", requestedPath)
	}
}
