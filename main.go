//go:build cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	abiVersion = 1
	pluginID   = "cpa-freetier-plugin"

	// OpenCode v2 Endpoints
	opencodeChatURL      = "https://opencode.ai/inference/v1/chat/completions"
	opencodeResponsesURL = "https://opencode.ai/inference/v1/responses"
	opencodeClientUA     = "opencode/latest/2.0.26/cli"

	// Kilo Endpoints
	kiloChatURL = "https://api.kilo.ai/api/gateway/chat/completions"
)

const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const sessionPrefixMask = (uint64(1) << 48) - 1

var (
	proxyIndex     uint32
	sessionMu      sync.Mutex
	lastSessionTs  int64
	sessionCounter int64
	cachedSessions = make(map[string]sessionEntry)
)

type sessionEntry struct {
	sessionID string
	lastUsed  time.Time
}

func logMsg(format string, args ...any) {
	f, err := os.OpenFile("/tmp/cpa_plugin.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err == nil {
		defer f.Close()
		fmt.Fprintf(f, "[%s] "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
}

func getOpenCodeProxy() *url.URL {
	// 1. Prioritas utama: Environment variable eksplisit jika user/developer menentukan sendiri
	if p := os.Getenv("OPENCODE_PROXY"); p != "" {
		if u, err := url.Parse(p); err == nil {
			return u
		}
	}

	// 2. Fallback otomatis untuk VM / host yang memiliki local proxy pool (misal Xray, Tinyproxy, dsb)
	// Hanya jika port tersebut benar-benar LISTEN dan bisa dihubungi dalam 60ms.
	ports := []string{"8127", "8124", "8125", "8123"}
	idx := atomic.AddUint32(&proxyIndex, 1)
	for i := 0; i < len(ports); i++ {
		port := ports[(int(idx)+i)%len(ports)]
		addr := "127.0.0.1:" + port
		conn, err := net.DialTimeout("tcp", addr, 60*time.Millisecond)
		if err == nil {
			conn.Close()
			u, _ := url.Parse("http://" + addr)
			return u
		}
	}

	// 3. User umum (tanpa proxy): Mengembalikan nil agar request langsung konek direct (atau lewat HTTP_PROXY sistem)
	return nil
}

func getHTTPClient(forOpenCode bool) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}
	if forOpenCode {
		if p := getOpenCodeProxy(); p != nil {
			transport.Proxy = http.ProxyURL(p)
		} else {
			transport.Proxy = http.ProxyFromEnvironment
		}
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{
		Timeout:   90 * time.Second,
		Transport: transport,
	}
}

var openCodeModels = map[string]bool{
	"glm-5.3-flash-free":              true,
	"big-pickle":                      true,
	"deepseek-v4.1-flash-free":        true,
	"space-bunny-free":                true,
	"cat-jam-free":                    true,
	"nemotron-3.5-lightning-free":     true,
	"muse-spark-1.2-contributor-free": true,
	"muse-spark-1.3-contributor-free": true,
}

var kiloModels = map[string]bool{
	"kilo-auto/free":                                    true,
	"poolside/laguna-s-2.1:free":                         true,
	"poolside/laguna-xs-2.1:free":                        true,
	"cohere/north-mini-code:free":                       true,
	"nvidia/nemotron-3-ultra-550b-a55b:free":            true,
	"nvidia/nemotron-3-super-120b-a12b:free":            true,
	"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free": true,
	"openrouter/free":                                   true,
	"dots-studio/dots-3-note-preview:free":               true,
	"liquid/lfm-2.5-2.6b:free":                           true,
	"nvidia/nemotron-3.5-lightning:free":                 true,
	"nvidia/nemotron-3.5-super-vl:free":                 true,
	"thinkingmachines/inkling-small:free":                true,
	"nvidia/nemotron-3.5-content-safety:free":            true,
	"nex-agi/nex-n2.5-pro:free":                          true,
	"nex-agi/nex-n2.5-mini:free":                         true,
	"stepfun/step-3.7-flash:free":                       true,
	"stealth/space-bunny-alpha":                          true,
	"inclusionai/ling-3.0-flash-fin:free":                true,
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *pluginError    `json:"error,omitempty"`
}

type pluginError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type executorRequest struct {
	Model           string `json:"Model"`
	OriginalRequest []byte `json:"OriginalRequest"`
	Payload         []byte `json:"Payload"`
	StreamID        string `json:"stream_id,omitempty"`
}

type streamEmit struct {
	StreamID string `json:"stream_id"`
	Payload  []byte `json:"payload,omitempty"`
	Error    string `json:"error,omitempty"`
}

type streamClose struct {
	StreamID string `json:"stream_id"`
	Error    string `json:"error,omitempty"`
}

type upstreamResponse struct {
	status int
	header http.Header
	body   io.ReadCloser
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	logMsg("plugin initialized")
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required", 0))
		return 1
	}
	var raw []byte
	if request != nil && requestLen > 0 {
		raw = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	methodStr := C.GoString(method)
	out, err := handleMethod(methodStr, raw)
	if err != nil {
		logMsg("handleMethod error for %s: %v", methodStr, err)
		writeResponse(response, errorEnvelope("plugin_error", err.Error(), 0))
		return 1
	}
	writeResponse(response, out)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	logMsg("plugin shutdown")
}

func handleMethod(method string, raw []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return ok(map[string]any{
			"schema_version": 1,
			"metadata": map[string]any{
				"Name":             pluginID,
				"Version":          "1.2.0",
				"Author":           "tsaQB",
				"GitHubRepository": "https://github.com/tsaQB/cpa-freetier-plugin",
				"ConfigFields":     []any{},
			},
			"capabilities": map[string]any{
				"model_provider":         true,
				"executor":               true,
				"executor_model_scope":   "static",
				"executor_input_formats":  []string{"chat-completions"},
				"executor_output_formats": []string{"chat-completions"},
				"request_normalizer":     true,
			},
		})
	case "request.normalize":
		return normalizeRequest(raw)
	case "model.static", "model.for_auth":
		return ok(map[string]any{
			"Provider": pluginID,
			"Models":   listModels(),
		})
	case "executor.identifier":
		return ok(map[string]string{
			"identifier": pluginID,
		})
	case "executor.execute":
		return execute(raw)
	case "executor.execute_stream":
		return executeStream(raw)
	case "executor.count_tokens":
		return ok(map[string]any{
			"Payload": []byte(`{"total_tokens":0}`),
		})
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method, 0), nil
	}
}

type requestTransformRequest struct {
	FromFormat string          `json:"FromFormat"`
	ToFormat   string          `json:"ToFormat"`
	Model      string          `json:"Model"`
	Stream     bool            `json:"Stream"`
	Body       json.RawMessage `json:"Body"`
}

type payloadResponse struct {
	Body json.RawMessage `json:"Body"`
}

func normalizeRequest(payload []byte) ([]byte, error) {
	var req requestTransformRequest
	if len(payload) > 0 {
		if errDecode := json.Unmarshal(payload, &req); errDecode != nil {
			return nil, errDecode
		}
	}
	if len(req.Body) == 0 {
		return ok(payloadResponse{Body: req.Body})
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(req.Body, &bodyMap); err != nil {
		return ok(payloadResponse{Body: req.Body})
	}

	modelLower := strings.ToLower(strings.TrimSpace(req.Model))
	changed := false

	// Clean parameters that conflict with Kilo AI or OpenCode
	if strings.Contains(modelLower, ":free") || strings.HasPrefix(modelLower, "kilo-auto") || openCodeModels[modelLower] {
		if _, exists := bodyMap["reasoning_effort"]; exists {
			delete(bodyMap, "reasoning_effort")
			changed = true
		}
		if _, exists := bodyMap["thinking"]; exists {
			delete(bodyMap, "thinking")
			changed = true
		}
	}

	if changed {
		newBody, err := json.Marshal(bodyMap)
		if err == nil {
			return ok(payloadResponse{Body: json.RawMessage(newBody)})
		}
	}

	return ok(payloadResponse{Body: req.Body})
}

func listModels() []map[string]any {
	type modelDef struct {
		id, name     string
		context, max int64
		thinking     bool
	}

	items := []modelDef{
		// OpenCode Free Tier (9router v2 protocol)
		{"glm-5.3-flash-free", "GLM 5.3 Flash Free (OpenCode)", 200000, 32768, true},
		{"big-pickle", "Big Pickle Free (OpenCode)", 200000, 32000, true},
		{"deepseek-v4.1-flash-free", "DeepSeek V4.1 Flash Free (OpenCode)", 1000000, 384000, true},
		{"space-bunny-free", "Space Bunny Free (OpenCode)", 1048576, 131072, true},
		{"cat-jam-free", "Cat Jam Free (OpenCode)", 1048576, 131072, true},
		{"nemotron-3.5-lightning-free", "Nemotron 3.5 Lightning Free (OpenCode)", 1000000, 65536, false},
		{"muse-spark-1.2-contributor-free", "Muse Spark 1.2 Contributor Free", 1048576, 131072, true},
		{"muse-spark-1.3-contributor-free", "Muse Spark 1.3 Contributor Free", 1048576, 131072, true},

		// Kilo Free Tier
		{"kilo-auto/free", "Kilo Auto Free", 256000, 10000, false},
		{"poolside/laguna-s-2.1:free", "Poolside Laguna S 2.1 Free", 262144, 32768, false},
		{"poolside/laguna-xs-2.1:free", "Laguna XS 2.1 Free (Kilo)", 262144, 32768, false},
		{"cohere/north-mini-code:free", "North Mini Code Free (Kilo)", 256000, 64000, false},
		{"nvidia/nemotron-3-ultra-550b-a55b:free", "Nemotron 3 Ultra 550B Free (Kilo)", 1000000, 65536, true},
		{"nvidia/nemotron-3-super-120b-a12b:free", "Nemotron 3 Super 120B Free (Kilo)", 1000000, 65536, true},
		{"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", "Nemotron 3 Nano Omni Free (Kilo)", 256000, 65536, true},
		{"openrouter/free", "OpenRouter Free Auto (Kilo)", 200000, 65536, false},
		{"dots-studio/dots-3-note-preview:free", "Dots3-Note Preview Free (Kilo)", 512000, 65536, false},
		{"liquid/lfm-2.5-2.6b:free", "LiquidAI LFM2.5 2.6B Free (Kilo)", 65536, 16384, false},
		{"nvidia/nemotron-3.5-lightning:free", "Nemotron 3.5 Lightning Free (Kilo)", 1000000, 65536, false},
		{"nex-agi/nex-n2.5-pro:free", "Nex AGI N2.5 Pro Free", 256000, 32000, true},
		{"nex-agi/nex-n2.5-mini:free", "Nex AGI N2.5 Mini Free", 256000, 32000, false},
		{"stepfun/step-3.7-flash:free", "Step 3.7 Flash Free (Kilo)", 262144, 262144, false},
		{"stealth/space-bunny-alpha", "Space Bunny Alpha (1M)", 1000000, 65536, false},
		{"inclusionai/ling-3.0-flash-fin:free", "Ling 3.0 Flash Fin Free (Kilo)", 262144, 32768, false},
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"ID":                         item.id,
			"Object":                     "model",
			"OwnedBy":                    pluginID,
			"Type":                       pluginID,
			"DisplayName":                item.name,
			"SupportedGenerationMethods": []string{"chat"},
			"ContextLength":              item.context,
			"MaxCompletionTokens":        item.max,
			"UserDefined":                true,
		})
	}
	return out
}

func execute(raw []byte) ([]byte, error) {
	req, err := decodeRequest(raw)
	if err != nil {
		return errorEnvelope("invalid_request", err.Error(), 400), nil
	}
	logMsg("execute start: model=%s", req.Model)
	resp, err := callUpstream(req.Model, req.Payload, false)
	if err != nil {
		logMsg("execute callUpstream err: %v", err)
		return errorEnvelope("upstream_error", err.Error(), 502), nil
	}
	defer resp.body.Close()

	var bodyBuf bytes.Buffer
	scanner := bufio.NewScanner(resp.body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	isSSE := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") {
			isSSE = true
		}
		bodyBuf.WriteString(line)
		bodyBuf.WriteByte('\n')

		if trimmed == "data: [DONE]" || trimmed == "[DONE]" {
			break
		}
	}

	body := bodyBuf.Bytes()
	if isSSE {
		body = assembleSSE(body, req.Model)
	}

	logMsg("execute complete: model=%s, len=%d", req.Model, len(body))
	return ok(map[string]any{
		"Payload": body,
		"Headers": responseHeaders(resp.header),
	})
}

func executeStream(raw []byte) ([]byte, error) {
	req, err := decodeRequest(raw)
	if err != nil {
		return errorEnvelope("invalid_request", err.Error(), 400), nil
	}
	if strings.TrimSpace(req.StreamID) == "" {
		return errorEnvelope("executor_error", "stream_id is required", 0), nil
	}

	logMsg("executeStream start: model=%s, stream_id=%s", req.Model, req.StreamID)
	go func() {
		resp, callErr := callUpstream(req.Model, req.Payload, true)
		if callErr != nil {
			logMsg("executeStream callUpstream error: %v", callErr)
			closeStream(req.StreamID, callErr.Error())
			return
		}
		defer resp.body.Close()

		scanner := bufio.NewScanner(resp.body)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, ":") {
				continue
			}

			// Clean double data prefix
			cleaned := line
			if strings.HasPrefix(cleaned, "data:") {
				cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "data:"))
			}
			if strings.HasPrefix(cleaned, "data:") {
				cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "data:"))
			}

			if cleaned == "[DONE]" || cleaned == "data: [DONE]" {
				break
			}

			// Restore tool names if any were renamed
			cleaned = restoreToolNamesInLine(cleaned)

			if errEmit := emitStream(req.StreamID, []byte(cleaned)); errEmit != nil {
				logMsg("executeStream emitStream error: %v", errEmit)
				closeStream(req.StreamID, errEmit.Error())
				return
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			logMsg("executeStream scanner error: %v", scanErr)
			closeStream(req.StreamID, scanErr.Error())
			return
		}
		closeStream(req.StreamID, "")
		logMsg("executeStream closed successfully: stream_id=%s", req.StreamID)
	}()

	return ok(map[string]any{
		"Headers": http.Header{"Content-Type": []string{"text/event-stream"}},
	})
}

func restoreToolNamesInLine(line string) string {
	if strings.Contains(line, "read_client") {
		line = strings.ReplaceAll(line, "read_client", "read")
	}
	if strings.Contains(line, "shell_client") {
		line = strings.ReplaceAll(line, "shell_client", "shell")
	}
	return line
}

func decodeRequest(raw []byte) (executorRequest, error) {
	var req executorRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, err
	}
	if strings.TrimSpace(req.Model) == "" {
		return req, fmt.Errorf("model is required")
	}
	if len(req.Payload) == 0 {
		if len(req.OriginalRequest) > 0 {
			req.Payload = req.OriginalRequest
		} else {
			return req, fmt.Errorf("request payload is required")
		}
	}
	return req, nil
}

func callUpstream(model string, payload []byte, stream bool) (*upstreamResponse, error) {
	cleanModel := strings.TrimPrefix(strings.TrimPrefix(model, "opencode/"), "kilo/")
	if kiloModels[model] || kiloModels[cleanModel] {
		return callKilo(model, payload, stream)
	}
	return callOpenCode(cleanModel, payload, stream)
}

func callOpenCode(model string, payload []byte, stream bool) (*upstreamResponse, error) {
	baseURL := strings.TrimRight(os.Getenv("OPENCODE_BASE_URL"), "/")
	targetURL := opencodeChatURL
	if baseURL != "" {
		targetURL = baseURL + "/inference/v1/chat/completions"
	} else if model == "muse-spark-1.3-contributor-free" || model == "muse-spark-1.2-contributor-free" {
		targetURL = opencodeResponsesURL
	}

	logMsg("callOpenCode: targetURL=%s, payloadLen=%d, rawPayload=%s", targetURL, len(payload), string(payload))
	transformedPayload := cloakOpencodePayload(payload, targetURL == opencodeResponsesURL)
	sessionID := getOrCreateSession("opencode_global")
	logMsg("callOpenCode: sessionID=%s, transformedLen=%d", sessionID, len(transformedPayload))

	headers := http.Header{
		"Content-Type":          []string{"application/json"},
		"Authorization":         []string{"Bearer public"},
		"User-Agent":            []string{opencodeClientUA},
		"x-opencode-client":     []string{"cli"},
		"x-opencode-project":    []string{"global"},
		"x-opencode-session-id": []string{sessionID},
		"x-opencode-session":    []string{sessionID},
		"x-session-affinity":    []string{sessionID},
		"x-session-id":          []string{sessionID},
		"Accept":                []string{"text/event-stream"},
	}

	proxy := getOpenCodeProxy()
	logMsg("callOpenCode: selected proxy=%v", proxy)
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}
	client := &http.Client{
		Transport: transport,
		// Do not set global Timeout for streaming SSE! It aborts long responses or hangs Do!
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(transformedPayload))
	if err != nil {
		return nil, err
	}
	req.Header = headers

	logMsg("callOpenCode: sending Do(req)...")
	res, err := client.Do(req)
	logMsg("callOpenCode: Do(req) returned err=%v, res=%v", err, res != nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return nil, fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return &upstreamResponse{status: res.StatusCode, header: res.Header, body: res.Body}, nil
}

// Official OpenCode v2.0.26 fingerprint tools verbatim from 9router
var officialFingerprintTools = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name":        "read",
			"description": "Read the contents of a file or directory. Supports text files, images, and PDFs. Images and PDFs are presented directly to the model. Each text line is prefixed by its 1-based line number as <line>: <content>. The prefix is for reference and is not part of the file content. Directory entries are returned one per line. Use offset and limit to read large files or directories in sections. Prefer one larger read over many small slices, and use grep to find specific content in large files.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "File or directory to read"},
					"offset": map[string]any{"type": "integer", "minimum": 0, "description": "The line or directory entry to start reading from (1-based)"},
					"limit":  map[string]any{"type": "integer", "minimum": 0, "description": "The maximum number of lines or directory entries to read (defaults to and capped at 2000)"},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		},
	},
	{
		"type": "function",
		"function": map[string]any{
			"name":        "shell",
			"description": "Execute a shell command and return its output. Commands run on Linux using bash. Quote file paths containing spaces or special characters. Prefer dedicated tools over shell commands when possible. When output is large, the full result is saved to a file and a truncated preview is returned. Rely on automatic truncation unless filtering the output is more useful. Commands accept an optional timeout, background commands have no timeout by default. Background commands return immediately, and you will be notified when they complete.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":    map[string]any{"type": "string", "description": "Shell command string to execute"},
					"workdir":    map[string]any{"type": "string", "description": "Working directory to execute the command in. Defaults to the current working directory. When possible, avoid changing directories in the command and set the working directory here instead."},
					"timeout":    map[string]any{"type": "integer", "minimum": 0, "description": "Timeout in milliseconds. Set to 0 to disable the timeout. Defaults to 120000 for foreground commands. Background commands have no timeout by default."},
					"background": map[string]any{"type": "boolean", "description": "Run the command in the background and return immediately (useful for dev servers and long-running builds). You do not need to use '&' at the end of the command when using this parameter. You will be notified when it completes. DO NOT poll for completion."},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			},
		},
	},
}

func cloakOpencodePayload(raw []byte, isResponses bool) []byte {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}

	if m, ok := body["model"].(string); ok {
		body["model"] = strings.TrimPrefix(m, "opencode/")
	}

	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}

	existingTools, _ := body["tools"].([]any)
	var finalTools []any
	namesPresent := make(map[string]bool)

	for _, t := range existingTools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, isFn := tm["function"].(map[string]any)
		name := ""
		if isFn {
			name, _ = fn["name"].(string)
		} else {
			name, _ = tm["name"].(string)
		}
		name = strings.TrimSpace(name)
		if name == "read" {
			name = "read_client"
			if isFn {
				fn["name"] = name
			} else {
				tm["name"] = name
			}
		} else if name == "shell" {
			name = "shell_client"
			if isFn {
				fn["name"] = name
			} else {
				tm["name"] = name
			}
		}
		namesPresent[name] = true
		finalTools = append(finalTools, tm)
	}

	for _, ft := range officialFingerprintTools {
		fn := ft["function"].(map[string]any)
		n := fn["name"].(string)
		if isResponses {
			finalTools = append(finalTools, map[string]any{
				"type":        "function",
				"name":        n,
				"description": fn["description"],
				"parameters":  fn["parameters"],
			})
		} else {
			finalTools = append(finalTools, ft)
		}
	}

	body["tools"] = finalTools
	body["tool_choice"] = "auto"

	note := "Do not call the `read` or `shell` tools; they are placeholders that do nothing. Answer the prompt directly or use other tools."
	if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
		firstMsg, _ := msgs[0].(map[string]any)
		if firstMsg != nil && firstMsg["role"] == "system" {
			if content, ok := firstMsg["content"].(string); ok {
				firstMsg["content"] = content + "\n\n" + note
			}
		} else {
			body["messages"] = append([]any{map[string]any{"role": "system", "content": note}}, msgs...)
		}
	}

	out, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return out
}

func callKilo(model string, payload []byte, stream bool) (*upstreamResponse, error) {
	headers := http.Header{
		"Content-Type": []string{"application/json"},
	}
	if stream {
		headers.Set("Accept", "text/event-stream")
	}

	client := getHTTPClient(false)
	req, err := http.NewRequest(http.MethodPost, kiloChatURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header = headers

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return nil, fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return &upstreamResponse{status: res.StatusCode, header: res.Header, body: res.Body}, nil
}

func descendingTimeHex(ts int64, counter int64) string {
	val := uint64(^(ts*0x1000 + counter)) & sessionPrefixMask
	return fmt.Sprintf("%012x", val)
}

func genSessionIDLocked() string {
	now := time.Now().UnixMilli()
	if now != lastSessionTs {
		lastSessionTs = now
		sessionCounter = 0
	}
	sessionCounter++

	timeHex := descendingTimeHex(now, sessionCounter)
	randBytes := make([]byte, 14)
	_, _ = rand.Read(randBytes)
	for i := range randBytes {
		randBytes[i] = base62Chars[int(randBytes[i])%len(base62Chars)]
	}
	return fmt.Sprintf("ses_%s%s", timeHex, string(randBytes))
}

func genSessionID() string {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	return genSessionIDLocked()
}

func getOrCreateSession(key string) string {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	now := time.Now()
	if entry, ok := cachedSessions[key]; ok {
		if now.Sub(entry.lastUsed) < 30*time.Minute {
			entry.lastUsed = now
			cachedSessions[key] = entry
			return entry.sessionID
		}
	}
	id := genSessionIDLocked()
	cachedSessions[key] = sessionEntry{sessionID: id, lastUsed: now}
	return id
}

func assembleSSE(raw []byte, fallbackModel string) []byte {
	lines := strings.Split(string(raw), "\n")
	var resID string
	var created int64
	model := fallbackModel
	var contentParts []string
	var reasoningParts []string
	finishReason := "stop"
	var usage any

	type toolCallObj struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	toolCalls := make(map[int]*toolCallObj)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(line[5:])
		if dataStr == "[DONE]" {
			break
		}
		var chunk struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
			Model   string `json:"model"`
			Usage   any    `json:"usage"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
			if resID == "" && chunk.ID != "" {
				resID = chunk.ID
			}
			if created == 0 && chunk.Created != 0 {
				created = chunk.Created
			}
			if chunk.Model != "" {
				model = chunk.Model
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}
				if choice.Delta.Content != "" {
					contentParts = append(contentParts, choice.Delta.Content)
				}
				if choice.Delta.ReasoningContent != "" {
					reasoningParts = append(reasoningParts, choice.Delta.ReasoningContent)
				}
				for _, tc := range choice.Delta.ToolCalls {
					obj, exists := toolCalls[tc.Index]
					if !exists {
						obj = &toolCallObj{Type: "function"}
						toolCalls[tc.Index] = obj
					}
					if tc.ID != "" {
						obj.ID = tc.ID
					}
					if tc.Function.Name != "" {
						name := tc.Function.Name
						if name == "read_client" {
							name = "read"
						} else if name == "shell_client" {
							name = "shell"
						}
						obj.Function.Name = name
					}
					if tc.Function.Arguments != "" {
						obj.Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}
	}

	msg := map[string]any{
		"role": "assistant",
	}
	if len(contentParts) > 0 {
		msg["content"] = strings.Join(contentParts, "")
	} else {
		msg["content"] = nil
	}
	if len(reasoningParts) > 0 {
		msg["reasoning_content"] = strings.Join(reasoningParts, "")
	}
	if len(toolCalls) > 0 {
		var list []*toolCallObj
		for i := 0; i < len(toolCalls); i++ {
			if t, ok := toolCalls[i]; ok {
				list = append(list, t)
			}
		}
		msg["tool_calls"] = list
	}

	if resID == "" {
		resID = "chatcmpl-cpa-freetier"
	}
	if created == 0 {
		created = time.Now().Unix()
	}

	respObj := map[string]any{
		"id":      resID,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
	}
	if usage != nil {
		respObj["usage"] = usage
	}

	out, err := json.Marshal(respObj)
	if err != nil {
		return raw
	}
	return out
}

func emitStream(streamID string, payload []byte) error {
	req := streamEmit{StreamID: streamID, Payload: payload}
	rawPayload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	cMethod := C.CString("host.stream.emit")
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload == nil {
			return fmt.Errorf("allocate stream emit payload")
		}
		defer C.free(cPayload)
		requestPtr = (*C.uint8_t)(cPayload)
	}

	rc := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if rc != 0 {
		return fmt.Errorf("host stream_emit failed: %d", rc)
	}
	return nil
}

func closeStream(streamID, errStr string) {
	req := streamClose{StreamID: streamID, Error: errStr}
	rawPayload, _ := json.Marshal(req)
	cMethod := C.CString("host.stream.close")
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload != nil {
			defer C.free(cPayload)
			requestPtr = (*C.uint8_t)(cPayload)
		}
	}

	C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
}

func ok(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: b})
}

func errorEnvelope(code, message string, httpStatus int) []byte {
	b, _ := json.Marshal(envelope{
		OK: false,
		Error: &pluginError{
			Code:       code,
			Message:    message,
			HTTPStatus: httpStatus,
		},
	})
	return b
}

func responseHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string)
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v
		}
	}
	return out
}

func writeResponse(buf *C.cliproxy_buffer, b []byte) {
	if buf == nil || len(b) == 0 {
		return
	}
	buf.ptr = C.CBytes(b)
	buf.len = C.size_t(len(b))
}
