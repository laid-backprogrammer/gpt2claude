package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

func (a *App) maybeShortCircuit(w http.ResponseWriter, req map[string]any) bool {
	messages, _ := req["messages"].([]any)
	tools, _ := req["tools"].([]any)
	stream := boolValue(req["stream"])
	model := a.currentConfig().Model

	if intNumber(req["max_tokens"]) == 1 && len(messages) == 1 {
		if message, ok := messages[0].(map[string]any); ok && message["role"] == "user" {
			if strings.Contains(strings.ToLower(contentText(message["content"])), "quota") {
				respondText(w, req, model, "Quota check passed.", false)
				return true
			}
		}
	}

	text := ""
	if len(messages) == 1 {
		if message, ok := messages[0].(map[string]any); ok && message["role"] == "user" {
			text = contentText(message["content"])
		}
	}

	if strings.Contains(text, "<policy_spec>") && strings.Contains(text, "Command:") {
		command := text[strings.LastIndex(text, "Command:")+len("Command:"):]
		respondText(w, req, model, extractCommandPrefix(strings.TrimSpace(command)), false)
		return true
	}

	system := strings.ToLower(systemText(req["system"]))
	if system != "" && len(tools) == 0 && strings.Contains(system, "title") {
		titlePrompt := strings.Contains(system, "sentence-case title") ||
			(strings.Contains(system, "return json") && strings.Contains(system, "field") &&
				(strings.Contains(system, "coding session") || strings.Contains(system, "this session")))
		if titlePrompt {
			respondText(w, req, model, "Conversation", false)
			return true
		}
	}

	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok || message["role"] != "user" {
			continue
		}
		if strings.Contains(contentText(message["content"]), "[SUGGESTION MODE:") {
			respondText(w, req, model, "", false)
			return true
		}
	}

	if text != "" && len(tools) == 0 && strings.Contains(text, "Command:") && strings.Contains(text, "Output:") {
		lower := strings.ToLower(text)
		systemHasExtract := strings.Contains(system, "extract any file paths") ||
			strings.Contains(system, "file paths that this command")
		if strings.Contains(lower, "filepaths") || strings.Contains(lower, "<filepaths>") || systemHasExtract {
			command := between(text, "Command:", "Output:")
			respondText(w, req, model, extractFilepathsFromCommand(strings.TrimSpace(command)), false)
			return true
		}
	}

	_ = stream
	return false
}

func extractCommandPrefix(command string) string {
	if strings.Contains(command, "`") || strings.Contains(command, "$(") {
		return "command_injection_detected"
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "none"
	}
	for len(parts) > 0 && strings.Contains(parts[0], "=") && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`).MatchString(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return "none"
	}
	first := parts[0]
	twoWord := map[string]bool{"git": true, "npm": true, "docker": true, "kubectl": true, "cargo": true, "go": true, "pip": true, "yarn": true}
	if twoWord[first] && len(parts) > 1 && !strings.HasPrefix(parts[1], "-") {
		return first + " " + parts[1]
	}
	return first
}

func extractFilepathsFromCommand(command string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "<filepaths>\n</filepaths>"
	}
	base := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	baseParts := strings.Split(base, "/")
	base = baseParts[len(baseParts)-1]
	if base == "cat" || base == "head" || base == "tail" || base == "less" || base == "more" || base == "bat" || base == "type" {
		files := []string{}
		for _, part := range parts[1:] {
			if !strings.HasPrefix(part, "-") {
				files = append(files, part)
			}
		}
		if len(files) > 0 {
			return "<filepaths>\n" + strings.Join(files, "\n") + "\n</filepaths>"
		}
	}
	return "<filepaths>\n</filepaths>"
}

func between(text, start, end string) string {
	s := strings.Index(text, start)
	if s < 0 {
		return ""
	}
	s += len(start)
	e := strings.Index(text[s:], end)
	if e < 0 {
		return text[s:]
	}
	return text[s : s+e]
}

func normalizeTaskInput(name string, input map[string]any) map[string]any {
	if name == "Task" && input["run_in_background"] != false {
		copyInput := map[string]any{}
		for k, v := range input {
			copyInput[k] = v
		}
		copyInput["run_in_background"] = false
		return copyInput
	}
	return input
}

func normalizeTaskJSON(name, partial string) string {
	if name != "Task" || partial == "" {
		return partial
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(partial), &input); err != nil {
		return partial
	}
	normalized := normalizeTaskInput(name, input)
	out, _ := json.Marshal(normalized)
	return string(out)
}

func intNumber(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
