package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type ClaudeConfigPayload struct {
	UpstreamBaseURL string `json:"upstream_base_url"`
	UpstreamAPIKey  string `json:"upstream_api_key"`
	BaseURL         string `json:"base_url"`
	AuthToken       string `json:"auth_token"`
	Model           string `json:"model"`
	FastModel       string `json:"fast_model"`
	SubagentModel   string `json:"subagent_model"`
	EffortLevel     string `json:"effort_level"`
}

func (a *App) handleClaudeConfigAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path, _ := claudeUserSettingsPath()
		runtimePath, _ := runtimeConfigPath()
		cfg := a.currentConfig()
		writeJSON(w, http.StatusOK, map[string]any{
			"path":             path,
			"runtime_path":     runtimePath,
			"has_upstream_key": cfg.APIKey != "",
			"defaults":         a.defaultClaudeConfigPayload(),
			"settings":         a.claudeSettingsPreview(a.defaultClaudeConfigPayload()),
			"exports":          shellExports(a.defaultClaudeConfigPayload()),
		})
	case http.MethodPost:
		defer r.Body.Close()
		payload := a.defaultClaudeConfigPayload()
		_ = json.NewDecoder(r.Body).Decode(&payload)
		payload = normalizeClaudeConfigPayload(payload, a.defaultClaudeConfigPayload())
		if err := a.updateRuntimeConfig(payload.UpstreamBaseURL, payload.UpstreamAPIKey, payload.Model); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		path, backup, err := writeClaudeUserSettings(a.claudeSettingsPreview(payload))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		runtimePath, _ := runtimeConfigPath()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path, "backup": backup, "runtime_path": runtimePath})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (a *App) defaultClaudeConfigPayload() ClaudeConfigPayload {
	cfg := a.currentConfig()
	model := firstNonEmpty(cfg.Model, "gpt-5.5")
	return ClaudeConfigPayload{
		UpstreamBaseURL: cfg.BaseURL,
		UpstreamAPIKey:  "",
		BaseURL:         a.localBaseURL(),
		AuthToken:       "test",
		Model:           model,
		FastModel:       model,
		SubagentModel:   model,
		EffortLevel:     "max",
	}
}

func (a *App) localBaseURL() string {
	cfg := a.currentConfig()
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, cfg.Port)
}

func (a *App) claudeSettingsPreview(payload ClaudeConfigPayload) map[string]any {
	payload = normalizeClaudeConfigPayload(payload, a.defaultClaudeConfigPayload())
	return map[string]any{
		"model":       payload.Model,
		"effortLevel": payload.EffortLevel,
		"env": map[string]string{
			"ANTHROPIC_BASE_URL":             payload.BaseURL,
			"ANTHROPIC_AUTH_TOKEN":           payload.AuthToken,
			"ANTHROPIC_MODEL":                payload.Model,
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   payload.Model,
			"ANTHROPIC_DEFAULT_SONNET_MODEL": payload.Model,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  payload.FastModel,
			"CLAUDE_CODE_SUBAGENT_MODEL":     payload.SubagentModel,
			"CLAUDE_CODE_EFFORT_LEVEL":       payload.EffortLevel,
		},
	}
}

func normalizeClaudeConfigPayload(payload, defaults ClaudeConfigPayload) ClaudeConfigPayload {
	if payload.UpstreamBaseURL == "" {
		payload.UpstreamBaseURL = defaults.UpstreamBaseURL
	}
	if payload.BaseURL == "" {
		payload.BaseURL = defaults.BaseURL
	}
	if payload.AuthToken == "" {
		payload.AuthToken = defaults.AuthToken
	}
	if payload.Model == "" {
		payload.Model = defaults.Model
	}
	if payload.FastModel == "" {
		payload.FastModel = payload.Model
	}
	if payload.SubagentModel == "" {
		payload.SubagentModel = payload.FastModel
	}
	if payload.EffortLevel == "" {
		payload.EffortLevel = defaults.EffortLevel
	}
	return payload
}

func shellExports(payload ClaudeConfigPayload) string {
	return fmt.Sprintf(`export ANTHROPIC_BASE_URL=%q
export ANTHROPIC_AUTH_TOKEN=%q
export ANTHROPIC_MODEL=%q
export ANTHROPIC_DEFAULT_OPUS_MODEL=%q
export ANTHROPIC_DEFAULT_SONNET_MODEL=%q
export ANTHROPIC_DEFAULT_HAIKU_MODEL=%q
export CLAUDE_CODE_SUBAGENT_MODEL=%q
export CLAUDE_CODE_EFFORT_LEVEL=%q`,
		payload.BaseURL,
		payload.AuthToken,
		payload.Model,
		payload.Model,
		payload.Model,
		payload.FastModel,
		payload.SubagentModel,
		payload.EffortLevel,
	)
}

func writeClaudeUserSettings(patch map[string]any) (string, string, error) {
	path, err := claudeUserSettingsPath()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}

	settings := map[string]any{}
	var backup string
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		backup = fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, existing, 0o600); err != nil {
			return "", "", err
		}
		if err := json.Unmarshal(existing, &settings); err != nil {
			return "", "", fmt.Errorf("existing settings.json is not valid JSON: %w", err)
		}
	}

	mergeClaudeSettings(settings, patch)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", "", err
	}
	return path, backup, nil
}

func mergeClaudeSettings(dst map[string]any, patch map[string]any) {
	for key, value := range patch {
		if key == "env" {
			dstEnv, _ := dst["env"].(map[string]any)
			if dstEnv == nil {
				dstEnv = map[string]any{}
			}
			switch src := value.(type) {
			case map[string]string:
				for k, v := range src {
					dstEnv[k] = v
				}
			case map[string]any:
				for k, v := range src {
					dstEnv[k] = v
				}
			}
			dst["env"] = dstEnv
			continue
		}
		dst[key] = value
	}
}

func claudeUserSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}
