package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Host    string
	Port    int
	BaseURL string
	APIKey  string
	Model   string
}

func main() {
	cfg := loadConfig()

	mux := http.NewServeMux()
	app := &App{cfg: cfg, client: &http.Client{Timeout: 90 * time.Second}}

	mux.HandleFunc("/", app.handleGUI)
	mux.HandleFunc("/health", app.handleHealth)
	mux.HandleFunc("/api/claude-config", app.handleClaudeConfigAPI)
	mux.HandleFunc("/v1/messages", app.handleMessages)
	mux.HandleFunc("/v1/messages/count_tokens", app.handleCountTokens)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("gpt2claude-lite listening on http://%s", addr)
	log.Printf("upstream=%s model=%s", cfg.BaseURL, cfg.Model)
	if err := http.ListenAndServe(addr, requestLog(mux)); err != nil {
		log.Fatal(err)
	}
}

type App struct {
	cfg    Config
	client *http.Client
	mu     sync.RWMutex
}

func loadConfig() Config {
	saved := loadSavedRuntimeConfig()
	host := flag.String("host", envOr("G2C_HOST", "127.0.0.1"), "listen host")
	port := flag.Int("port", envInt("PORT", envInt("G2C_PORT", 43501)), "listen port")
	baseURL := flag.String("base-url", envOr("OPENAI_BASE_URL", envOr("OPENAI_TEST_BASE", saved.BaseURL)), "OpenAI-compatible base URL")
	model := flag.String("model", envOr("G2C_MODEL", envOr("TARGET_MODEL", firstNonEmpty(saved.Model, "gpt-5.5"))), "upstream model")
	flag.Parse()

	key := firstNonEmpty(firstEnv("OPENAI_API_KEY", "G2C_API_KEY"), saved.APIKey)
	return Config{
		Host:    *host,
		Port:    *port,
		BaseURL: strings.TrimRight(*baseURL, "/"),
		APIKey:  key,
		Model:   normalizeModel(*model),
	}
}

func (a *App) currentConfig() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) updateRuntimeConfig(baseURL, apiKey, model string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if baseURL != "" {
		a.cfg.BaseURL = strings.TrimRight(baseURL, "/")
	}
	if apiKey != "" {
		a.cfg.APIKey = apiKey
	}
	if model != "" {
		a.cfg.Model = normalizeModel(model)
	}
	return saveRuntimeConfig(RuntimeConfig{
		BaseURL: a.cfg.BaseURL,
		APIKey:  a.cfg.APIKey,
		Model:   a.cfg.Model,
	})
}

type RuntimeConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

func runtimeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gpt2claude-lite", "config.json"), nil
}

func loadSavedRuntimeConfig() RuntimeConfig {
	path, err := runtimeConfigPath()
	if err != nil {
		return RuntimeConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}
	}
	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return RuntimeConfig{}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.Model = normalizeModel(cfg.Model)
	return cfg
}

func saveRuntimeConfig(cfg RuntimeConfig) error {
	path, err := runtimeConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func normalizeModel(model string) string {
	model = strings.TrimSpace(model)
	model = strings.TrimPrefix(model, "responses/")
	if after, ok := strings.CutPrefix(model, "openai/"); ok {
		return after
	}
	return model
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	cfg := a.currentConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"base_url": cfg.BaseURL,
		"model":    cfg.Model,
		"has_key":  cfg.APIKey != "",
	})
}

func (a *App) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if handleProbe(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": estimateTokens(body)})
}

func (a *App) handleMessages(w http.ResponseWriter, r *http.Request) {
	if handleProbe(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	defer r.Body.Close()
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if handled := a.maybeShortCircuit(w, req); handled {
		return
	}
	if handled := a.maybeHandleWebServerTool(w, req); handled {
		return
	}
	cfg := a.currentConfig()
	if cfg.APIKey == "" {
		respondText(w, req, cfg.Model, "Missing upstream API key. Open the GUI and configure Base URL + API key.", false)
		return
	}
	if err := a.callResponses(w, req); err != nil {
		log.Printf("upstream error: %v", err)
		respondText(w, req, cfg.Model, providerErrorText(err), true)
	}
}

func handleProbe(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return false
	}
	w.Header().Set("Allow", "POST, HEAD, OPTIONS")
	w.WriteHeader(http.StatusNoContent)
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
