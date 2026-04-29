package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranslateResponsesJSONNormalizesTask(t *testing.T) {
	app := &App{cfg: Config{Model: "gpt-5.5"}}
	got := app.translateResponsesJSON(map[string]any{
		"id":     "resp_1",
		"model":  "gpt-5.5",
		"status": "completed",
		"usage":  map[string]any{"input_tokens": float64(10), "output_tokens": float64(3)},
		"output": []any{
			map[string]any{
				"type":      "function_call",
				"id":        "fc_1",
				"call_id":   "call_1",
				"name":      "Task",
				"arguments": `{"description":"demo","prompt":"echo hi","run_in_background":true}`,
			},
		},
	})
	content := got["content"].([]map[string]any)
	input := content[0]["input"].(map[string]any)
	if input["run_in_background"] != false {
		t.Fatalf("Task run_in_background was not normalized: %#v", input)
	}
	if got["stop_reason"] != "tool_use" {
		t.Fatalf("expected tool_use stop reason, got %#v", got["stop_reason"])
	}
}

func TestTranslateResponsesStreamBuffersTaskArguments(t *testing.T) {
	app := &App{cfg: Config{Model: "gpt-5.5"}}
	body := strings.NewReader(strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`event: response.output_item.added`,
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"Task"}}`,
		``,
		`event: response.function_call_arguments.delta`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"description\":\"demo\",\"prompt\":\"echo hi\",\"run_in_background\":true}"}`,
		``,
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"Task"}}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":3},"output":[{"type":"function_call"}]}}`,
		``,
	}, "\n"))
	rec := httptest.NewRecorder()
	if err := app.translateResponsesStream(rec, body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	text := rec.Body.String()
	if !strings.Contains(text, `\"run_in_background\":false`) {
		t.Fatalf("missing normalized false in SSE:\n%s", text)
	}
	if strings.Contains(text, `\"run_in_background\":true`) {
		t.Fatalf("leaked true in SSE:\n%s", text)
	}
	if !strings.Contains(text, `"stop_reason":"tool_use"`) {
		t.Fatalf("missing tool_use stop reason:\n%s", text)
	}
}

func TestBuildResponsesRequestTranslatesThinking(t *testing.T) {
	app := &App{cfg: Config{Model: "gpt-5.5"}}
	req := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"thinking": map[string]any{"type": "adaptive", "effort": "max"},
	}
	got := app.buildResponsesRequest(req)
	reasoning := got["reasoning"].(map[string]any)
	if reasoning["effort"] != "xhigh" {
		t.Fatalf("expected max to map to xhigh, got %#v", reasoning)
	}
}
