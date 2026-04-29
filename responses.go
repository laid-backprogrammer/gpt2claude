package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (a *App) callResponses(w http.ResponseWriter, anthropicReq map[string]any) error {
	cfg := a.currentConfig()
	payload := a.buildResponsesRequest(anthropicReq)
	body, _ := json.Marshal(payload)

	upstreamReq, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", "application/json")
	if boolValue(anthropicReq["stream"]) {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := a.client.Do(upstreamReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(text))
	}

	if boolValue(anthropicReq["stream"]) {
		return a.translateResponsesStream(w, resp.Body)
	}

	var upstream map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, a.translateResponsesJSON(upstream))
	return nil
}

func (a *App) buildResponsesRequest(req map[string]any) map[string]any {
	cfg := a.currentConfig()
	out := map[string]any{
		"model": cfg.Model,
		"input": translateMessages(req["messages"]),
		"store": false,
	}
	if system := systemText(req["system"]); system != "" {
		out["instructions"] = system
	}
	if maxTokens := intNumber(req["max_tokens"]); maxTokens > 0 {
		out["max_output_tokens"] = maxTokens
	}
	for _, key := range []string{"temperature", "top_p"} {
		if value, ok := req[key]; ok {
			out[key] = value
		}
	}
	if tools := translateTools(req["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := translateToolChoice(req["tool_choice"]); choice != nil {
		out["tool_choice"] = choice
	}
	if reasoning := translateThinking(req["thinking"], req["output_config"]); reasoning != nil {
		out["reasoning"] = reasoning
	}
	if boolValue(req["stream"]) {
		out["stream"] = true
	}
	return out
}

func translateMessages(value any) []any {
	messages, _ := value.([]any)
	input := []any{}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(message["role"])
		content := message["content"]
		if role == "user" {
			if text, ok := content.(string); ok {
				input = append(input, map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}})
				continue
			}
			parts := []any{}
			for _, blockRaw := range arrayValue(content) {
				block, ok := blockRaw.(map[string]any)
				if !ok {
					continue
				}
				switch block["type"] {
				case "text":
					parts = append(parts, map[string]any{"type": "input_text", "text": stringValue(block["text"])})
				case "tool_result":
					input = append(input, map[string]any{
						"type":    "function_call_output",
						"call_id": stringValue(block["tool_use_id"]),
						"output":  toolResultText(block["content"]),
					})
				}
			}
			if len(parts) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "user", "content": parts})
			}
		}
		if role == "assistant" {
			if text, ok := content.(string); ok {
				input = append(input, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}})
				continue
			}
			parts := []any{}
			for _, blockRaw := range arrayValue(content) {
				block, ok := blockRaw.(map[string]any)
				if !ok {
					continue
				}
				switch block["type"] {
				case "text":
					parts = append(parts, map[string]any{"type": "output_text", "text": stringValue(block["text"])})
				case "tool_use":
					args, _ := json.Marshal(block["input"])
					input = append(input, map[string]any{
						"type":      "function_call",
						"call_id":   stringValue(block["id"]),
						"name":      stringValue(block["name"]),
						"arguments": string(args),
					})
				}
			}
			if len(parts) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "assistant", "content": parts})
			}
		}
	}
	return input
}

func translateTools(value any) []any {
	tools := []any{}
	for _, raw := range arrayValue(value) {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(tool["name"])
		toolType := stringValue(tool["type"])
		if name == "web_search" || strings.HasPrefix(toolType, "web_search") {
			tools = append(tools, map[string]any{"type": "web_search_preview"})
			continue
		}
		converted := map[string]any{"type": "function", "name": name}
		if desc := stringValue(tool["description"]); desc != "" {
			converted["description"] = desc
		}
		if schema, ok := tool["input_schema"]; ok {
			converted["parameters"] = schema
		}
		tools = append(tools, converted)
	}
	return tools
}

func translateToolChoice(value any) map[string]any {
	choice, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	switch stringValue(choice["type"]) {
	case "any":
		return map[string]any{"type": "required"}
	case "tool":
		return map[string]any{"type": "function", "name": stringValue(choice["name"])}
	default:
		return map[string]any{"type": "auto"}
	}
}

func translateThinking(thinkingValue any, outputConfigValue any) map[string]any {
	thinking := mapValue(thinkingValue)
	if len(thinking) == 0 {
		return nil
	}
	thinkingType := stringValue(thinking["type"])
	effort := ""
	if thinkingType == "adaptive" {
		outputConfig := mapValue(outputConfigValue)
		effort = firstNonEmpty(
			stringValue(thinking["effort"]),
			stringValue(outputConfig["effort"]),
			"medium",
		)
	} else if thinkingType == "enabled" {
		budget := intNumber(thinking["budget_tokens"])
		switch {
		case budget >= 10000:
			effort = "high"
		case budget >= 5000:
			effort = "medium"
		default:
			effort = "low"
		}
	}
	effort = normalizeReasoningEffort(effort)
	if effort == "" {
		return nil
	}
	return map[string]any{"effort": effort}
}

func normalizeReasoningEffort(effort string) string {
	switch strings.ToLower(effort) {
	case "max":
		return "xhigh"
	case "xhigh", "high", "medium", "low":
		return strings.ToLower(effort)
	default:
		return ""
	}
}

func (a *App) translateResponsesJSON(resp map[string]any) map[string]any {
	cfg := a.currentConfig()
	content := []map[string]any{}
	stopReason := "end_turn"
	for _, raw := range arrayValue(resp["output"]) {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch item["type"] {
		case "message":
			for _, partRaw := range arrayValue(item["content"]) {
				part, ok := partRaw.(map[string]any)
				if ok && part["type"] == "output_text" {
					content = append(content, map[string]any{"type": "text", "text": stringValue(part["text"])})
				}
			}
		case "function_call":
			input := map[string]any{}
			_ = json.Unmarshal([]byte(stringValue(item["arguments"])), &input)
			name := stringValue(item["name"])
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    firstNonEmpty(stringValue(item["call_id"]), stringValue(item["id"])),
				"name":  name,
				"input": normalizeTaskInput(name, input),
			})
			stopReason = "tool_use"
		}
	}
	usage := mapValue(resp["usage"])
	return map[string]any{
		"id":            firstNonEmpty(stringValue(resp["id"]), "msg_"+randID()),
		"type":          "message",
		"role":          "assistant",
		"model":         firstNonEmpty(stringValue(resp["model"]), cfg.Model),
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  intNumber(usage["input_tokens"]),
			"output_tokens": intNumber(usage["output_tokens"]),
		},
	}
}

func (a *App) translateResponsesStream(w http.ResponseWriter, body io.Reader) error {
	s := newSSE(w)
	cfg := a.currentConfig()
	state := &streamState{model: cfg.Model, messageID: "msg_" + randID(), currentIndex: -1, itemToIndex: map[string]int{}, itemToName: map[string]string{}, taskBuffers: map[string]string{}}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	event := ""
	dataLines := []string{}
	flush := func() error {
		if len(dataLines) == 0 {
			event = ""
			return nil
		}
		dataText := strings.Join(dataLines, "\n")
		eventName := event
		event = ""
		dataLines = nil
		if dataText == "[DONE]" {
			return nil
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(dataText), &payload); err != nil {
			return nil
		}
		state.process(eventName, payload, s)
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !state.sentStop {
		state.ensureStart(s)
		s.send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}})
		s.send("message_stop", map[string]any{"type": "message_stop"})
	}
	return nil
}

type streamState struct {
	model        string
	messageID    string
	currentIndex int
	itemToIndex  map[string]int
	itemToName   map[string]string
	taskBuffers  map[string]string
	currentTool  string
	sentStart    bool
	sentStop     bool
	toolSeen     bool
}

func (s *streamState) ensureStart(out sseWriter) {
	if s.sentStart {
		return
	}
	s.sentStart = true
	out.send("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            s.messageID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         s.model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
}

func (s *streamState) nextIndex() int {
	s.currentIndex++
	return s.currentIndex
}

func (s *streamState) process(event string, payload map[string]any, out sseWriter) {
	eventType := stringValue(payload["type"])
	if eventType == "" {
		eventType = event
	}
	if eventType == "" {
		return
	}
	if eventType == "response.created" {
		s.ensureStart(out)
		return
	}
	s.ensureStart(out)
	switch eventType {
	case "response.output_item.added":
		item := mapValue(payload["item"])
		itemType := stringValue(item["type"])
		itemID := firstNonEmpty(stringValue(item["id"]), stringValue(item["call_id"]))
		if itemID == "" {
			itemID = fmt.Sprintf("item_%d", s.currentIndex+1)
		}
		index := s.nextIndex()
		s.itemToIndex[itemID] = index
		if itemType == "message" {
			out.send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}})
		}
		if itemType == "function_call" {
			name := stringValue(item["name"])
			callID := firstNonEmpty(stringValue(item["call_id"]), itemID)
			s.itemToName[itemID] = name
			s.currentTool = itemID
			s.toolSeen = true
			out.send("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": index,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    callID,
					"name":  name,
					"input": map[string]any{},
				},
			})
		}
	case "response.output_text.delta":
		itemID := stringValue(payload["item_id"])
		index := s.indexFor(itemID)
		out.send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": stringValue(payload["delta"])}})
	case "response.function_call_arguments.delta":
		itemID := firstNonEmpty(stringValue(payload["item_id"]), s.currentTool)
		name := s.itemToName[itemID]
		delta := stringValue(payload["delta"])
		if name == "Task" {
			s.taskBuffers[itemID] += delta
			return
		}
		out.send("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.indexFor(itemID), "delta": map[string]any{"type": "input_json_delta", "partial_json": delta}})
	case "response.output_item.done":
		item := mapValue(payload["item"])
		itemID := firstNonEmpty(stringValue(item["id"]), stringValue(item["call_id"]), s.currentTool)
		index := s.indexFor(itemID)
		if buffered := s.taskBuffers[itemID]; buffered != "" {
			out.send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": normalizeTaskJSON("Task", buffered)}})
			delete(s.taskBuffers, itemID)
		}
		out.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	case "response.completed", "response.failed", "response.incomplete":
		stopReason := "end_turn"
		response := mapValue(payload["response"])
		if stringValue(response["status"]) == "incomplete" {
			stopReason = "max_tokens"
		}
		for _, raw := range arrayValue(response["output"]) {
			if item, ok := raw.(map[string]any); ok && item["type"] == "function_call" {
				stopReason = "tool_use"
				break
			}
		}
		if s.toolSeen {
			stopReason = "tool_use"
		}
		usage := mapValue(response["usage"])
		out.send("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
			"usage": map[string]any{"input_tokens": intNumber(usage["input_tokens"]), "output_tokens": intNumber(usage["output_tokens"])},
		})
		out.send("message_stop", map[string]any{"type": "message_stop"})
		s.sentStop = true
	}
}

func (s *streamState) indexFor(itemID string) int {
	if itemID != "" {
		if index, ok := s.itemToIndex[itemID]; ok {
			return index
		}
	}
	return s.currentIndex
}

func arrayValue(value any) []any {
	if value == nil {
		return nil
	}
	if v, ok := value.([]any); ok {
		return v
	}
	return nil
}

func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if v, ok := value.(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func toolResultText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, raw := range v {
			if block, ok := raw.(map[string]any); ok && block["type"] == "text" {
				parts = append(parts, stringValue(block["text"]))
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	default:
		return stringValue(value)
	}
}

func compactJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func providerErrorText(err error) string {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "429") || strings.Contains(text, "rate limit") {
		return "Provider rate limit reached. Please retry."
	}
	if strings.Contains(text, "502") || strings.Contains(text, "503") || strings.Contains(text, "504") || strings.Contains(text, "overloaded") || strings.Contains(text, "capacity") {
		return "Provider is currently overloaded. Please retry."
	}
	if errors.Is(err, io.EOF) {
		return "Provider request failed. Please retry."
	}
	return "Provider request failed. Please retry."
}
