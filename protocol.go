package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func anthropicMessage(model string, content []map[string]any, stopReason string, inputTokens, outputTokens int) map[string]any {
	return map[string]any{
		"id":            "msg_" + randID(),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}
}

func respondText(w http.ResponseWriter, req map[string]any, model, text string, asProviderError bool) {
	stream := boolValue(req["stream"])
	response := anthropicMessage(
		model,
		[]map[string]any{{"type": "text", "text": text}},
		"end_turn",
		estimateTokens(req),
		max(1, len(text)/4),
	)
	if stream {
		writeAnthropicTextStream(w, response)
		return
	}
	status := http.StatusOK
	if !asProviderError && strings.HasPrefix(text, "Missing ") {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, response)
}

func writeAnthropicTextStream(w http.ResponseWriter, response map[string]any) {
	s := newSSE(w)
	message := response
	content := []map[string]any{}
	if raw, ok := message["content"].([]map[string]any); ok {
		content = raw
	}
	text := ""
	if len(content) > 0 {
		text, _ = content[0]["text"].(string)
	}
	msg := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            message["id"],
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         message["model"],
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 1,
			},
		},
	}
	s.send("message_start", msg)
	s.send("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	if text != "" {
		s.send("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": text},
		})
	}
	s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	s.send("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"input_tokens": 0, "output_tokens": max(1, len(text)/4)},
	})
	s.send("message_stop", map[string]any{"type": "message_stop"})
}

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSE(w http.ResponseWriter) sseWriter {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	return sseWriter{w: w, flusher: flusher}
}

func (s sseWriter) send(event string, data any) {
	payload, _ := json.Marshal(data)
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if block["type"] == "text" {
					parts = append(parts, stringValue(block["text"]))
				}
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func systemText(value any) string {
	return contentText(value)
}

func latestUserText(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message, ok := messages[i].(map[string]any)
		if !ok || message["role"] != "user" {
			continue
		}
		return contentText(message["content"])
	}
	return ""
}

func estimateTokens(value any) int {
	b, _ := json.Marshal(value)
	n := len(b) / 4
	if n < 1 {
		return 1
	}
	return n
}

func boolValue(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func randID() string {
	return strings.ReplaceAll(fmt.Sprintf("%d", time.Now().UnixNano()), "-", "")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
