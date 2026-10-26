package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestBuildOmniRushResponsesPayloadTranslatesMessages(t *testing.T) {
	chat := []byte(`{
		"model": "gpt-6-astra",
		"messages": [
			{"role":"system","content":"You are terse."},
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi"},
			{"role":"user","content":[{"type":"text","text":"again"},{"type":"image_url","image_url":{"url":"https://x/y.png"}}]}
		],
		"temperature": 0.3,
		"max_tokens": 999,
		"stream": false
	}`)

	raw, err := buildOmniRushResponsesPayload(chat, "gpt-6-astra")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if body["stream"] != true {
		t.Errorf("expected stream=true, got %v", body["stream"])
	}
	if body["store"] != false {
		t.Errorf("expected store=false, got %v", body["store"])
	}
	if _, ok := body["max_output_tokens"]; ok {
		t.Error("expected no output token cap: OmniRush models do not support it")
	}
	if got := body["instructions"]; got != "You are terse." {
		t.Errorf("expected system message folded into instructions, got %v", got)
	}

	input, ok := body["input"].([]any)
	if !ok {
		t.Fatalf("expected input array, got %T", body["input"])
	}
	if len(input) != 3 {
		t.Fatalf("expected 3 input items (user, assistant, user), got %d", len(input))
	}

	first := input[0].(map[string]any)
	if first["role"] != "user" || first["type"] != "message" {
		t.Errorf("unexpected first item: %#v", first)
	}
	parts := first["content"].([]any)
	if parts[0].(map[string]any)["type"] != "input_text" {
		t.Errorf("expected input_text part, got %#v", parts[0])
	}

	second := input[1].(map[string]any)
	if second["role"] != "assistant" {
		t.Errorf("expected assistant item, got %#v", second)
	}
	if second["content"].([]any)[0].(map[string]any)["type"] != "output_text" {
		t.Errorf("expected output_text part for assistant, got %#v", second["content"])
	}

	third := input[2].(map[string]any)
	thirdParts := third["content"].([]any)
	if len(thirdParts) != 2 {
		t.Fatalf("expected text + image parts, got %d", len(thirdParts))
	}
	if thirdParts[1].(map[string]any)["type"] != "input_image" {
		t.Errorf("expected input_image part, got %#v", thirdParts[1])
	}
	if thirdParts[1].(map[string]any)["image_url"] != "https://x/y.png" {
		t.Errorf("expected flattened image url, got %#v", thirdParts[1])
	}
}

func TestBuildOmniRushResponsesPayloadMapsToolsAndCalls(t *testing.T) {
	chat := []byte(`{
		"model": "gpt-6-sol",
		"messages": [
			{"role":"user","content":"read the file"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.txt\"}"}}
			]},
			{"role":"tool","tool_call_id":"call_1","content":"file body"}
		],
		"tools":[{"type":"function","function":{"name":"read","description":"Read a file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],
		"tool_choice":"auto",
		"reasoning_effort":"high"
	}`)

	raw, err := buildOmniRushResponsesPayload(chat, "gpt-6-sol")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %#v", body["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "read" {
		t.Errorf("expected flat responses tool shape, got %#v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Error("responses tools must not nest a function object")
	}

	if body["tool_choice"] != "auto" {
		t.Errorf("expected tool_choice auto, got %v", body["tool_choice"])
	}

	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Errorf("expected reasoning effort high, got %#v", body["reasoning"])
	}

	input := body["input"].([]any)
	var sawCall, sawOutput bool
	for _, item := range input {
		m := item.(map[string]any)
		switch m["type"] {
		case "function_call":
			sawCall = true
			if m["call_id"] != "call_1" || m["name"] != "read" {
				t.Errorf("unexpected function_call item: %#v", m)
			}
		case "function_call_output":
			sawOutput = true
			if m["call_id"] != "call_1" || m["output"] != "file body" {
				t.Errorf("unexpected function_call_output item: %#v", m)
			}
		}
	}
	if !sawCall || !sawOutput {
		t.Errorf("expected both function_call and function_call_output items, got %#v", input)
	}
}

func TestTranslateOmniRushStreamEmitsChatChunks(t *testing.T) {
	sse := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-6-astra","status":"in_progress"}}`,
		``,
		`event: response.reasoning_summary_text.delta`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"OK"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"!"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-6-astra","usage":{"input_tokens":11,"output_tokens":2,"total_tokens":13,"input_tokens_details":{"cached_tokens":4}}}}`,
		``,
	}, "\n")

	out, err := io.ReadAll(translateOmniRushStream(strings.NewReader(sse), "gpt-6-astra"))
	if err != nil {
		t.Fatalf("translate stream: %v", err)
	}

	text := string(out)
	if !strings.Contains(text, "data: [DONE]") {
		t.Error("expected terminating [DONE] sentinel")
	}

	var content strings.Builder
	var reasoning string
	var finish string
	var usage map[string]any
	chunks := 0

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			continue
		}
		chunks++

		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("bad chunk %q: %v", payload, err)
		}
		if chunk["object"] != "chat.completion.chunk" {
			t.Errorf("expected chat.completion.chunk, got %v", chunk["object"])
		}
		if chunk["model"] != "gpt-6-astra" {
			t.Errorf("expected model to be carried through, got %v", chunk["model"])
		}

		choice := chunk["choices"].([]any)[0].(map[string]any)
		delta := choice["delta"].(map[string]any)
		if c, ok := delta["content"].(string); ok {
			content.WriteString(c)
		}
		if r, ok := delta["reasoning_content"].(string); ok {
			reasoning = r
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finish = fr
		}
		if u, ok := chunk["usage"].(map[string]any); ok {
			usage = u
		}
	}

	if content.String() != "OK!" {
		t.Errorf("expected assembled content %q, got %q", "OK!", content.String())
	}
	if reasoning != "thinking" {
		t.Errorf("expected reasoning passthrough, got %q", reasoning)
	}
	if finish != "stop" {
		t.Errorf("expected finish_reason stop, got %q", finish)
	}
	if chunks < 4 {
		t.Errorf("expected at least 4 chunks, got %d", chunks)
	}
	if usage == nil || usage["prompt_tokens"] != float64(11) || usage["completion_tokens"] != float64(2) {
		t.Errorf("expected translated usage, got %#v", usage)
	}
	if details, ok := usage["prompt_tokens_details"].(map[string]any); !ok || details["cached_tokens"] != float64(4) {
		t.Errorf("expected cached token details, got %#v", usage["prompt_tokens_details"])
	}
}

func TestTranslateOmniRushStreamToolCalls(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_9","name":"read"}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"path\":"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"a.txt\"}"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_2","model":"gpt-6-sol","usage":{"input_tokens":5,"output_tokens":3}}}`,
		``,
	}, "\n")

	out, err := io.ReadAll(translateOmniRushStream(strings.NewReader(sse), "gpt-6-sol"))
	if err != nil {
		t.Fatalf("translate stream: %v", err)
	}

	var finish string
	var args strings.Builder
	var sawToolName bool

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("bad chunk: %v", err)
		}
		choice := chunk["choices"].([]any)[0].(map[string]any)
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finish = fr
		}
		delta := choice["delta"].(map[string]any)
		calls, ok := delta["tool_calls"].([]any)
		if !ok {
			continue
		}
		call := calls[0].(map[string]any)
		fn := call["function"].(map[string]any)
		if name, ok := fn["name"].(string); ok && name == "read" {
			sawToolName = true
		}
		if fragment, ok := fn["arguments"].(string); ok {
			args.WriteString(fragment)
		}
	}

	if !sawToolName {
		t.Error("expected the tool name to be announced in a delta")
	}
	if args.String() != `{"path":"a.txt"}` {
		t.Errorf("expected reassembled arguments, got %q", args.String())
	}
	if finish != "tool_calls" {
		t.Errorf("expected finish_reason tool_calls, got %q", finish)
	}
}

func TestTranslateOmniRushStreamFailureEmitsError(t *testing.T) {
	sse := `data: {"type":"response.failed","response":{"error":{"message":"model_unavailable"}}}` + "\n\n"

	out, err := io.ReadAll(translateOmniRushStream(strings.NewReader(sse), "meta-muse-spark"))
	if err != nil {
		t.Fatalf("translate stream: %v", err)
	}
	if !strings.Contains(string(out), "model_unavailable") {
		t.Errorf("expected the upstream failure to surface, got %q", string(out))
	}
	if !strings.Contains(string(out), "[DONE]") {
		t.Error("expected the stream to terminate even after a failure")
	}
}

func TestOmniRushGatewayBaseNormalisation(t *testing.T) {
	cases := map[string]string{
		"https://omnirush.ai/omnirush":              "https://omnirush.ai/omnirush/v1",
		"https://omnirush.ai/omnirush/v1":           "https://omnirush.ai/omnirush/v1",
		"https://omnirush.ai/omnirush/v1/":          "https://omnirush.ai/omnirush/v1",
		"https://omnirush.ai/omnirush/v1/responses": "https://omnirush.ai/omnirush/v1",
	}
	for input, want := range cases {
		got := omniRushGatewayBase(&OmniRushAuth{GatewayURL: input})
		if got != want {
			t.Errorf("gateway base for %q = %q, want %q", input, got, want)
		}
	}
}

func TestOmniRushErrorDetailReadsBothShapes(t *testing.T) {
	if got := omniRushErrorDetail([]byte(`{"detail":"model_unavailable"}`)); got != "model_unavailable" {
		t.Errorf("detail shape: got %q", got)
	}
	if got := omniRushErrorDetail([]byte(`{"error":{"code":"official_client_required","message":"not supported"}}`)); got != "official_client_required: not supported" {
		t.Errorf("error shape: got %q", got)
	}
}
