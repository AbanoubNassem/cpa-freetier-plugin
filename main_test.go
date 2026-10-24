//go:build cgo

package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ── cloak: the free-tier gate is decided by payload shape ────────────────────
//
// This is the single most fragile part of the plugin. The OpenCode free tier
// rejects a request that does not carry the client's declared tools, so a
// refactor here silently breaks every free model. The assertions below pin the
// exact shape that is known to be accepted upstream.
func TestCloakOpencodePayloadKeepsGateFingerprint(t *testing.T) {
	raw := []byte(`{
		"model": "opencode/deepseek-v4.1-flash-free",
		"messages": [{"role": "system", "content": "base"}, {"role": "user", "content": "hi"}],
		"reasoning_effort": "high",
		"enable_thinking": true,
		"stream": false,
		"tools": [
			{"type": "function", "function": {"name": "read", "parameters": {"type": "object"}}},
			{"type": "function", "function": {"name": "shell", "parameters": {"type": "object"}}},
			{"type": "function", "function": {"name": "web_search", "parameters": {"type": "object"}}}
		]
	}`)

	out := cloakOpencodePayload(raw, false)

	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("cloaked payload is not valid JSON: %v", err)
	}

	if got := body["model"]; got != "deepseek-v4.1-flash-free" {
		t.Errorf("model prefix not stripped: %v", got)
	}
	if body["stream"] != true {
		t.Error("stream must be forced true")
	}
	so, ok := body["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Error("stream_options.include_usage is part of the accepted shape")
	}
	if body["tool_choice"] != "auto" {
		t.Error("tool_choice must be auto")
	}
	// enable_thinking is rejected upstream (400 invalid_request) so it is still
	// removed. reasoning_effort is accepted and must survive — see
	// TestSanitizerKeepsReasoningEffort.
	if _, present := body["enable_thinking"]; present {
		t.Error("enable_thinking should have been stripped")
	}
	if got := body["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort must be forwarded untouched, got %v", got)
	}

	// The official read/shell declarations must be present, and the caller's own
	// read/shell must be renamed so the two sets do not collide.
	names := map[string]bool{}
	tools, _ := body["tools"].([]any)
	for _, tw := range tools {
		tm, _ := tw.(map[string]any)
		fn, _ := tm["function"].(map[string]any)
		if n, ok := fn["name"].(string); ok {
			names[n] = true
		}
	}
	for _, required := range []string{"read", "shell", "web_search", "read_client", "shell_client"} {
		if !names[required] {
			t.Errorf("tool %q missing from cloaked payload (have %v)", required, names)
		}
	}

	// An operator note is appended so the model does not call the spoofed tools.
	msgs, _ := body["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if content, _ := first["content"].(string); !strings.Contains(content, "placeholders that do nothing") {
		t.Errorf("operator note missing from system message: %q", content)
	}
}

func TestCloakPrependsSystemNoteWhenAbsent(t *testing.T) {
	out := cloakOpencodePayload([]byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`), false)
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected a prepended system message, got %d messages", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("first message must be system, got %v", first["role"])
	}
}

// ── SSE assembly ────────────────────────────────────────────────────────────

func TestAssembleSSE(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"req-1","created":42,"model":"deepseek-v4.1-flash-free","choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_client","arguments":"{\"pa"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"total_tokens":9}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	out := assembleSSE([]byte(sse), "fallback-model")
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("assembled response is not valid JSON: %v", err)
	}
	if resp["model"] != "deepseek-v4.1-flash-free" {
		t.Errorf("model should come from the stream, got %v", resp["model"])
	}
	if resp["usage"] == nil {
		t.Error("usage chunk was dropped")
	}

	choices, _ := resp["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected one choice, got %d", len(choices))
	}
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason not propagated: %v", choice["finish_reason"])
	}
	msg, _ := choice["message"].(map[string]any)
	if msg["content"] != "Hello" {
		t.Errorf("content not concatenated: %v", msg["content"])
	}

	toolCalls, _ := msg["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	tc, _ := toolCalls[0].(map[string]any)
	fn, _ := tc["function"].(map[string]any)
	if fn["name"] != "read" {
		t.Errorf("read_client must be renamed back to read, got %v", fn["name"])
	}
	if fn["arguments"] != `{"path":"a"}` {
		t.Errorf("arguments not reassembled: %v", fn["arguments"])
	}
}

// ── model health ────────────────────────────────────────────────────────────

// TestMain keeps unit tests off the real log destination.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cpa-freetier-test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("CPA_FREETIER_LOG", filepath.Join(dir, "plugin.log"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestLooksModelMissing(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{`404 Not Found: {"error":"The requested model 'x' does not exist."}`, true},
		{`404 Not Found: `, true},
		{`410 Gone: {"error":{"type":"ModelDeprecated","message":"Model x is deprecated"}}`, true},
		{`400 Bad Request: {"error":{"message":"unknown model"}}`, true},
		{`500 Internal Server Error: `, false},
		{`429 Too Many Requests: {"error":{"message":"Rate limit exceeded"}}`, false},
		{`403 Forbidden: {"error":{"type":"FreeTierError"}}`, false},
		// A local error must never hide a model, and neither must a model answer
		// that merely mentions a status number.
		{`file does not exist`, false},
		{`dial tcp 127.0.0.1:8127: connection refused`, false},
		{`200 OK but the tool output mentioned 404`, false},
	}
	for _, c := range cases {
		if got := looksModelMissing(c.msg); got != c.want {
			t.Errorf("looksModelMissing(%.55q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

func TestModelHealthHidesAndRestores(t *testing.T) {
	const id = "test/model-that-vanished:free"
	clearHealth(id)
	t.Cleanup(func() { clearHealth(id) })

	if !modelHealthy(id) {
		t.Fatal("a fresh model must be healthy")
	}
	// A local/transport error is not evidence that the model is gone.
	markModelUnhealthy(id, os.ErrNotExist)
	if !modelHealthy(id) {
		t.Error("a local error must not hide a model")
	}
	markModelUnhealthy(id, errString("404 Not Found: does not exist"))
	if modelHealthy(id) {
		t.Error("a 404 must hide the model from the catalog")
	}
	for _, m := range listModels() {
		if m["ID"] == id {
			t.Error("hidden model is still advertised by listModels")
		}
	}
	markModelUnhealthy(id, nil)
	if !modelHealthy(id) {
		t.Error("a success must bring the model back")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func clearHealth(id string) {
	modelHealthMu.Lock()
	delete(unhealthyTill, id)
	modelHealthMu.Unlock()
}

// ── sessions ────────────────────────────────────────────────────────────────

var sessionIDPattern = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)

func TestSessionIDFormatAndReuse(t *testing.T) {
	first := getOrCreateSession("unit-test-key")
	second := getOrCreateSession("unit-test-key")
	if first != second {
		t.Errorf("warm session should be reused: %s != %s", first, second)
	}
	if !sessionIDPattern.MatchString(first) {
		t.Errorf("session id does not match the upstream format: %s", first)
	}

	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		id := genSessionID()
		if !sessionIDPattern.MatchString(id) {
			t.Fatalf("generated session id has the wrong shape: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate session id generated: %s", id)
		}
		seen[id] = true
	}
}

func TestExpiredSessionsAreSwept(t *testing.T) {
	sessionMu.Lock()
	cachedSessions["stale"] = sessionEntry{sessionID: "ses_deadbeef0000AAAAAAAAAAAAAA", lastUsed: time.Now().Add(-2 * sessionTTL)}
	sessionMu.Unlock()

	_ = getOrCreateSession("fresh-key")

	sessionMu.Lock()
	_, stillThere := cachedSessions["stale"]
	sessionMu.Unlock()
	if stillThere {
		t.Error("expired session entries must be deleted")
	}
}

// ── payload fingerprint & logging ───────────────────────────────────────────

func TestPayloadFingerprint(t *testing.T) {
	a := payloadFingerprint([]byte("secret conversation"))
	b := payloadFingerprint([]byte("secret conversation"))
	c := payloadFingerprint([]byte("another conversation"))
	if a != b {
		t.Error("fingerprint must be stable")
	}
	if a == c {
		t.Error("different payloads must produce different fingerprints")
	}
	if len(a) != 16 {
		t.Errorf("fingerprint should be 16 hex chars, got %d", len(a))
	}
	if strings.Contains(a, "secret") {
		t.Error("fingerprint must not leak payload content")
	}
}

func resetLogForTest(t *testing.T, path string) {
	t.Helper()
	logMu.Lock()
	if logHandle != nil {
		_ = logHandle.Close()
	}
	logHandle = nil
	logDisabled = false
	logSize = 0
	logPathUsed = ""
	logMu.Unlock()
	t.Setenv("CPA_FREETIER_LOG", path)
	t.Cleanup(func() {
		logMu.Lock()
		if logHandle != nil {
			_ = logHandle.Close()
		}
		logHandle = nil
		logDisabled = false
		logSize = 0
		logPathUsed = ""
		logMu.Unlock()
	})
}

func TestLogGoesToConfiguredPathAndNeverGrowsPastCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "plugin.log")
	resetLogForTest(t, path)

	if got := logFilePath(); got != path {
		t.Fatalf("CPA_FREETIER_LOG ignored: got %s want %s", got, path)
	}
	logMsg("hello %s", "world")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file was not created (parent dirs must be created): %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("log line missing: %q", string(data))
	}

	// Push the file past the cap and confirm rotation instead of unbounded growth.
	logMu.Lock()
	logSize = maxLogBytes + 1
	logMu.Unlock()
	logMsg("post-rotation line")

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected a rotated generation at %s.1: %v", path, err)
	}
	fresh, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("new log generation unreadable: %v", err)
	}
	if !strings.Contains(string(fresh), "post-rotation line") {
		t.Errorf("new generation should hold the latest line, got %q", string(fresh))
	}
	if int64(len(fresh)) > maxLogBytes {
		t.Errorf("rotated file already exceeds the cap: %d bytes", len(fresh))
	}
}

// ── request normalisation ───────────────────────────────────────────────────

func TestNormalizeRequestStripsUnsupportedFields(t *testing.T) {
	body := `{"model":"deepseek-v4.1-flash-free","messages":[],"thinking_budget":2048,"web_search":true}`
	raw, _ := json.Marshal(requestTransformRequest{
		Model: "deepseek-v4.1-flash-free",
		Body:  json.RawMessage(body),
	})

	out, err := normalizeRequest(raw)
	if err != nil {
		t.Fatalf("normalizeRequest failed: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	var resp payloadResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var cleaned map[string]any
	if err := json.Unmarshal(resp.Body, &cleaned); err != nil {
		t.Fatalf("normalised body is not valid JSON: %v", err)
	}
	for _, banned := range []string{"thinking_budget", "web_search"} {
		if _, present := cleaned[banned]; present {
			t.Errorf("%s should be stripped", banned)
		}
	}
	if cleaned["model"] != "deepseek-v4.1-flash-free" {
		t.Error("model must be preserved")
	}
}

// The upstream free-tier gateway accepts reasoning_effort (verified 200 OK on
// every advertised model) and it changes how much the model reasons, so it
// must not be removed from the request. See issue #7.
func TestSanitizerKeepsReasoningEffort(t *testing.T) {
	t.Setenv("CPA_STRIP_FIELDS", "")
	t.Setenv("CPA_KEEP_FIELDS", "")

	body := map[string]any{
		"model":            "deepseek-v4.1-flash-free",
		"reasoning_effort": "high",
		"enable_thinking":  true,
		"thinking_budget":  2048,
	}

	sanitizePayloadMap(body)

	if got := body["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort must survive sanitization, got %v", got)
	}
	for _, banned := range []string{"enable_thinking", "thinking_budget"} {
		if _, present := body[banned]; present {
			t.Errorf("%s must still be stripped (upstream rejects it)", banned)
		}
	}
}

// CPA_KEEP_FIELDS is the escape hatch for a deployment whose upstream does
// accept one of the normally-stripped parameters.
func TestKeepFieldsOverridesStripList(t *testing.T) {
	t.Setenv("CPA_STRIP_FIELDS", "")
	t.Setenv("CPA_KEEP_FIELDS", "enable_thinking, thinking_budget")

	body := map[string]any{
		"model":           "deepseek-v4.1-flash-free",
		"enable_thinking": true,
		"thinking_budget": 2048,
		"web_search":      true,
	}

	sanitizePayloadMap(body)

	for _, kept := range []string{"enable_thinking", "thinking_budget"} {
		if _, present := body[kept]; !present {
			t.Errorf("%s must be kept when listed in CPA_KEEP_FIELDS", kept)
		}
	}
	if _, present := body["web_search"]; present {
		t.Error("web_search is not in CPA_KEEP_FIELDS and must be stripped")
	}
}

// CPA_STRIP_FIELDS can add a field, and CPA_KEEP_FIELDS wins over it.
func TestKeepFieldsBeatsStripFields(t *testing.T) {
	t.Setenv("CPA_STRIP_FIELDS", "reasoning_effort")
	t.Setenv("CPA_KEEP_FIELDS", "reasoning_effort")

	body := map[string]any{"reasoning_effort": "low"}
	sanitizePayloadMap(body)

	if got := body["reasoning_effort"]; got != "low" {
		t.Errorf("CPA_KEEP_FIELDS must win over CPA_STRIP_FIELDS, got %v", got)
	}
}

func TestDatacenterEgressIsOptIn(t *testing.T) {
	t.Setenv("OPENCODE_ALLOW_DATACENTER", "")
	if datacenterAllowed() {
		t.Error("datacenter egress must be off unless explicitly enabled")
	}
	t.Setenv("OPENCODE_ALLOW_DATACENTER", "1")
	if !datacenterAllowed() {
		t.Error("datacenter opt-in was ignored")
	}
}

// proxyPool is what makes the built-in loopback pools harmless on a host that has
// none of them: no env var means the defaults (which simply fail to dial), an
// explicit empty value means "probe nothing".
func TestProxyPoolResolution(t *testing.T) {
	const env = "OPENCODE_PROXY_POOL"

	t.Run("defaults are used when unset", func(t *testing.T) {
		t.Setenv(env, "")
		os.Unsetenv(env)
		got := proxyPool(env, []string{"127.0.0.1:8127"})
		if len(got) != 1 || got[0].String() != "http://127.0.0.1:8127" {
			t.Fatalf("unexpected pool: %v", got)
		}
	})

	t.Run("empty value disables probing", func(t *testing.T) {
		t.Setenv(env, "")
		if got := proxyPool(env, []string{"127.0.0.1:8127"}); len(got) != 0 {
			t.Fatalf("an explicitly empty pool must probe nothing, got %v", got)
		}
	})

	t.Run("host:port gains a scheme and full URLs are preserved", func(t *testing.T) {
		t.Setenv(env, " 10.0.0.5:8080 , socks5h://127.0.0.1:1080 , ,http://user:pass@proxy.lan:3128 ")
		got := proxyPool(env, nil)
		want := []string{
			"http://10.0.0.5:8080",
			"socks5h://127.0.0.1:1080",
			"http://user:pass@proxy.lan:3128",
		}
		if len(got) != len(want) {
			t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i].String() != want[i] {
				t.Errorf("entry %d: got %s want %s", i, got[i], want[i])
			}
		}
	})

	t.Run("garbage entries are skipped", func(t *testing.T) {
		t.Setenv(env, "not a proxy,,:::")
		if got := proxyPool(env, nil); len(got) != 0 {
			t.Errorf("malformed entries must be dropped, got %v", got)
		}
	})
}

func TestProxyReachableRejectsUnusableEntries(t *testing.T) {
	for _, raw := range []string{"", "http://", "://missing-host"} {
		if u, err := url.Parse(raw); err == nil && proxyReachable(u) {
			t.Errorf("%q must not be considered reachable", raw)
		}
	}
}

// The fallback log destination must be portable instead of a hardcoded /tmp.
// All three temp-dir variables are set because os.TempDir() reads TMPDIR on
// Unix-likes and TMP/TEMP on Windows.
func TestLegacyLogPathIsPortable(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	got := legacyLogPath()
	if filepath.Dir(got) != filepath.Clean(os.TempDir()) {
		t.Errorf("legacy log path must follow os.TempDir(), got %s (TempDir=%s)", got, os.TempDir())
	}
	if filepath.Base(got) != "cpa_plugin.log" {
		t.Errorf("legacy log file name changed: %s", got)
	}
}

func TestDebugLoggingIsOptIn(t *testing.T) {
	t.Setenv("CPA_FREETIER_DEBUG", "")
	if debugEnabled() {
		t.Error("verbose logging must be off by default")
	}
	t.Setenv("CPA_FREETIER_DEBUG", "yes")
	if !debugEnabled() {
		t.Error("CPA_FREETIER_DEBUG=yes should enable verbose logging")
	}
}

func TestNormalizeRequestRepairsEmptyRoleAndInvalidMessages(t *testing.T) {
	// Issue #8: "Request normalizer: failed to convert messages: unexpected message at index 0: expected 'user', 'assistant' or 'system', got """
	body := `{"model":"free-mimo","messages":[{"role":"","content":"hello world"},{"role":"developer","content":"you are an assistant"},{"content":"test empty role omitted"}]}`
	raw, _ := json.Marshal(requestTransformRequest{
		Model: "free-mimo",
		Body:  json.RawMessage(body),
	})

	normRaw, err := normalizeRequest(raw)
	if err != nil {
		t.Fatalf("normalizeRequest failed: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(normRaw, &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}

	var resp payloadResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(resp.Body, &bodyMap); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	msgs, ok := bodyMap["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %v", msgs)
	}

	// Message 0 had empty role -> should be "user"
	msg0 := msgs[0].(map[string]any)
	if msg0["role"] != "user" {
		t.Errorf("msg0 role should be user, got %v", msg0["role"])
	}

	// Message 1 had "developer" role -> should be mapped to "system"
	msg1 := msgs[1].(map[string]any)
	if msg1["role"] != "system" {
		t.Errorf("msg1 role should be system, got %v", msg1["role"])
	}

	// Message 2 had missing role -> should be "user"
	msg2 := msgs[2].(map[string]any)
	if msg2["role"] != "user" {
		t.Errorf("msg2 role should be user, got %v", msg2["role"])
	}
}

func TestModelExclusionRules(t *testing.T) {
	// Issue #5: Support excluding models via config/env
	t.Setenv("CPA_EXCLUDED_MODELS", "muse*,kilo-auto/free,opencode/big-pickle")
	cachedExcludedRules = nil // clear cache

	if !isModelExcluded("muse-spark-1.3-contributor-free") {
		t.Error("expected muse-spark-1.3-contributor-free to be excluded by glob 'muse*'")
	}
	if !isModelExcluded("kilo-auto/free") {
		t.Error("expected kilo-auto/free to be excluded")
	}
	if !isModelExcluded("big-pickle") {
		t.Error("expected big-pickle to be excluded by opencode/big-pickle rule")
	}
	if isModelExcluded("mimo-v2.6-flash-free") {
		t.Error("mimo-v2.6-flash-free should NOT be excluded")
	}

	// Test listModels filtering
	models := listModels()
	for _, m := range models {
		id := m["ID"].(string)
		if id == "kilo-auto/free" || strings.HasPrefix(id, "muse") || id == "big-pickle" {
			t.Errorf("model %s should have been filtered out from listModels", id)
		}
	}
}
