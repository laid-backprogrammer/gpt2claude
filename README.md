# gpt2claude-lite

Small Anthropic Messages proxy for Claude Code against an OpenAI-compatible
Responses API upstream.

It intentionally keeps only the stable path:

```text
Claude Code -> /v1/messages -> OpenAI /v1/responses -> Anthropic response/SSE
```

## Run

### Native macOS app

Build the universal macOS app:

```bash
./scripts/build-macos-app.sh
```

Outputs:

```text
dist/GPT2Claude Lite.app
dist/GPT2Claude-Lite-macOS-universal.zip
```

The app is a SwiftUI macOS wrapper with the Go proxy embedded in
`Contents/Resources/gpt2claude-lite`. It runs on Apple Silicon and Intel Macs
with macOS 13+ and does not require Go to be installed on the target machine.

Because this local build is ad-hoc signed and not notarized, macOS Gatekeeper may
require right-clicking the app and choosing **Open** the first time.

### CLI server

```bash
export OPENAI_API_KEY=...
export OPENAI_BASE_URL=https://your-openai-compatible-host/v1
export G2C_MODEL=gpt-5.5
go run . --host 127.0.0.1 --port 43501
```

Open the built-in GUI at:

```text
http://127.0.0.1:43501/
```

The GUI can:

- Save upstream `base_url` / `api_key` / `model` to
  `~/.gpt2claude-lite/config.json`
- Generate shell exports
- Preview `settings.json`
- Write user-level Claude Code settings to `~/.claude/settings.json`

The upstream API key is not written into Claude Code settings; Claude Code talks
to the local proxy, and the proxy talks to the OpenAI-compatible upstream.

Claude Code env:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:43501
export ANTHROPIC_AUTH_TOKEN=test
export ANTHROPIC_MODEL=gpt-5.5
export ANTHROPIC_DEFAULT_OPUS_MODEL=gpt-5.5
export ANTHROPIC_DEFAULT_SONNET_MODEL=gpt-5.5
export ANTHROPIC_DEFAULT_HAIKU_MODEL=gpt-5.5
export CLAUDE_CODE_SUBAGENT_MODEL=gpt-5.5
export CLAUDE_CODE_EFFORT_LEVEL=max
```

## Scope

- `/v1/messages`
- `/v1/messages/count_tokens`
- HEAD/OPTIONS probes
- OpenAI Responses tool calls
- Task `run_in_background=false` normalization
- Claude Code quota/title/prefix/filepath/suggestion shortcuts
- Minimal streaming `web_search` / `web_fetch` server-tool shape
