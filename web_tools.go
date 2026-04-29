package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func (a *App) maybeHandleWebServerTool(w http.ResponseWriter, req map[string]any) bool {
	cfg := a.currentConfig()
	if !boolValue(req["stream"]) {
		return false
	}
	toolName := forcedServerToolName(req["tool_choice"])
	if toolName == "" || !hasToolNamed(req["tools"], toolName) {
		return false
	}
	messages, _ := req["messages"].([]any)
	text := latestUserText(messages)
	toolInput := map[string]any{}
	if toolName == "web_search" {
		toolInput["query"] = extractQuery(text)
	} else {
		toolInput["url"] = extractURL(text)
	}

	s := newSSE(w)
	toolID := "srvtoolu_" + randID()
	messageID := "msg_" + randID()
	s.send("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         cfg.Model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 1},
		},
	})
	s.send("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type":  "server_tool_use",
			"id":    toolID,
			"name":  toolName,
			"input": map[string]any{},
		},
	})
	inputJSON := compactJSON(toolInput)
	s.send("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": inputJSON},
	})
	s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})

	resultType := "web_search_tool_result"
	resultContent := any(map[string]any{"type": "web_search_tool_result_error", "error_code": "unavailable"})
	summary := "Web tool request failed."
	if toolName == "web_search" {
		query := stringValue(toolInput["query"])
		resultContent = []map[string]any{{"type": "web_search_result", "title": "Search handled locally", "url": "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)}}
		summary = "Search request captured: " + query
	} else {
		resultType = "web_fetch_tool_result"
		content, fetchSummary, err := a.runWebFetch(stringValue(toolInput["url"]))
		if err == nil {
			resultContent = content
			summary = fetchSummary
		} else {
			resultContent = map[string]any{"type": "web_fetch_tool_error", "error_code": "unavailable"}
			summary = "Web tool request failed."
		}
	}

	s.send("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 1,
		"content_block": map[string]any{
			"type":        resultType,
			"tool_use_id": toolID,
			"content":     resultContent,
		},
	})
	s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
	s.send("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         2,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	s.send("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 2,
		"delta": map[string]any{"type": "text_delta", "text": summary},
	})
	s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 2})
	usageKey := "web_search_requests"
	if toolName == "web_fetch" {
		usageKey = "web_fetch_requests"
	}
	s.send("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{
			"input_tokens":    0,
			"output_tokens":   max(1, len(summary)/4),
			"server_tool_use": map[string]any{usageKey: 1},
		},
	})
	s.send("message_stop", map[string]any{"type": "message_stop"})
	return true
}

func forcedServerToolName(value any) string {
	choice, ok := value.(map[string]any)
	if !ok || choice["type"] != "tool" {
		return ""
	}
	name := stringValue(choice["name"])
	if name == "web_search" || name == "web_fetch" {
		return name
	}
	return ""
}

func hasToolNamed(value any, name string) bool {
	tools, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		toolName := stringValue(tool["name"])
		toolType := stringValue(tool["type"])
		if toolName == name || strings.HasPrefix(toolType, name) {
			return true
		}
	}
	return false
}

func extractQuery(text string) string {
	re := regexp.MustCompile(`(?is)query:\s*(.+)`)
	if match := re.FindStringSubmatch(text); len(match) > 1 {
		return strings.Trim(match[1], "\"' \n\t")
	}
	return strings.TrimSpace(text)
}

func extractURL(text string) string {
	re := regexp.MustCompile(`https?://\S+`)
	if match := re.FindString(text); match != "" {
		return strings.TrimRight(match, ").,]")
	}
	return strings.TrimSpace(text)
}

func (a *App) runWebFetch(rawURL string) (map[string]any, string, error) {
	if err := validatePublicURL(rawURL); err != nil {
		return nil, "", err
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, "", errors.New("redirects are not followed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New(resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 24000))
	if err != nil {
		return nil, "", err
	}
	text := strings.TrimSpace(htmlToText(string(body)))
	if text == "" {
		text = strings.TrimSpace(string(body))
	}
	title := extractHTMLTitle(string(body))
	if title == "" {
		title = rawURL
	}
	content := map[string]any{
		"type":         "web_fetch_result",
		"url":          rawURL,
		"retrieved_at": time.Now().UTC().Format(time.RFC3339),
		"content": map[string]any{
			"type":   "document",
			"source": map[string]any{"type": "text", "media_type": "text/plain", "data": text},
			"title":  title,
			"citations": map[string]any{
				"enabled": true,
			},
		},
	}
	return content, text, nil
}

func validatePublicURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("only http and https are allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("host is required")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") {
		return errors.New("local hostnames are blocked")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return errors.New("non-public addresses are blocked")
		}
	}
	return nil
}

func htmlToText(html string) string {
	for _, pattern := range []string{
		`(?is)<script[^>]*>.*?</script>`,
		`(?is)<style[^>]*>.*?</style>`,
		`(?is)<noscript[^>]*>.*?</noscript>`,
	} {
		html = regexp.MustCompile(pattern).ReplaceAllString(html, " ")
	}
	re := regexp.MustCompile(`(?is)<[^>]+>`)
	html = re.ReplaceAllString(html, " ")
	re = regexp.MustCompile(`\s+`)
	return strings.TrimSpace(re.ReplaceAllString(html, " "))
}

func extractHTMLTitle(html string) string {
	re := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	if match := re.FindStringSubmatch(html); len(match) > 1 {
		return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(match[1], " "))
	}
	return ""
}
