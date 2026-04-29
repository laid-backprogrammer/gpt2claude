package main

import (
	"html/template"
	"net/http"
)

func (a *App) handleGUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/ui" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = guiTemplate.Execute(w, a.currentConfig())
}

var guiTemplate = template.Must(template.New("gui").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>gpt2claude-lite</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f6f8fb; color: #111827; }
    main { max-width: 1040px; margin: 0 auto; padding: 28px; }
    header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding-bottom: 18px; border-bottom: 1px solid #d8dee8; }
    h1 { font-size: 24px; margin: 0; font-weight: 680; letter-spacing: 0; }
    .status { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; padding: 6px 10px; border: 1px solid #a6d7b0; background: #eefaf1; border-radius: 6px; color: #17672a; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: #22a447; }
    section { padding: 22px 0; border-bottom: 1px solid #dfe5ee; }
    h2 { margin: 0 0 12px; font-size: 15px; font-weight: 680; }
    dl { display: grid; grid-template-columns: 160px minmax(0, 1fr); gap: 10px 18px; margin: 0; }
    dt { color: #5b6472; font-size: 13px; }
    dd { margin: 0; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; overflow-wrap: anywhere; }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px 16px; }
    label { display: grid; gap: 6px; color: #4b5563; font-size: 13px; }
    input, select { width: 100%; box-sizing: border-box; border: 1px solid #cbd5e1; border-radius: 6px; padding: 9px 10px; font: 13px ui-monospace, SFMono-Regular, Menlo, monospace; background: #fff; color: #111827; }
    textarea { width: 100%; box-sizing: border-box; min-height: 174px; resize: vertical; border: 1px solid #cbd5e1; border-radius: 8px; padding: 12px; background: #101827; color: #eef4ff; font: 13px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
    .actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 12px; }
    button { border: 1px solid #b8c4d6; background: #fff; border-radius: 6px; padding: 8px 12px; font-size: 13px; cursor: pointer; color: #111827; }
    button.primary { background: #1d4ed8; border-color: #1d4ed8; color: #fff; }
    button:hover { filter: brightness(0.97); }
    .message { min-height: 20px; margin-top: 10px; font-size: 13px; color: #2563eb; }
    @media (max-width: 760px) { main { padding: 18px; } .grid, dl { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
<main>
  <header>
    <h1>gpt2claude-lite</h1>
    <span class="status"><span class="dot"></span> running</span>
  </header>
  <section>
    <h2>Runtime</h2>
    <dl>
      <dt>Local endpoint</dt><dd>http://{{.Host}}:{{.Port}}/v1/messages</dd>
      <dt>Upstream</dt><dd>{{.BaseURL}}</dd>
      <dt>Model</dt><dd>{{.Model}}</dd>
    </dl>
  </section>
  <section>
    <h2>Upstream</h2>
    <div class="grid">
      <label>Base URL<input id="upstreamBaseUrl" placeholder="https://your-openai-compatible-host/v1"></label>
      <label>API key<input id="upstreamApiKey" type="password" placeholder="Leave blank to keep saved key"></label>
      <label>Model<input id="model" placeholder="gpt-5.5"></label>
      <label>Effort<select id="effort">
        <option value="max">max</option>
        <option value="xhigh">xhigh</option>
        <option value="high">high</option>
        <option value="medium">medium</option>
        <option value="low">low</option>
        <option value="auto">auto</option>
      </select></label>
    </div>
  </section>
  <section>
    <h2>Claude Code</h2>
    <div class="grid">
      <label>ANTHROPIC_BASE_URL<input id="baseUrl"></label>
      <label>ANTHROPIC_AUTH_TOKEN<input id="authToken"></label>
      <label>Fast model<input id="fastModel"></label>
      <label>Subagent model<input id="subagentModel"></label>
    </div>
    <div class="actions">
      <button onclick="copyText(exportsBox.value)">Copy exports</button>
      <button onclick="copyText(settingsBox.value)">Copy settings.json</button>
      <button class="primary" onclick="writeUserSettings()">One-click configure</button>
    </div>
    <div class="message" id="message"></div>
  </section>
  <section>
    <h2>Shell Exports</h2>
    <textarea id="exportsBox" spellcheck="false"></textarea>
  </section>
  <section>
    <h2>settings.json</h2>
    <textarea id="settingsBox" spellcheck="false"></textarea>
  </section>
</main>
<script>
const fields = {
  upstreamBaseUrl: document.getElementById('upstreamBaseUrl'),
  upstreamApiKey: document.getElementById('upstreamApiKey'),
  baseUrl: document.getElementById('baseUrl'),
  authToken: document.getElementById('authToken'),
  model: document.getElementById('model'),
  fastModel: document.getElementById('fastModel'),
  subagentModel: document.getElementById('subagentModel'),
  effort: document.getElementById('effort')
};
const exportsBox = document.getElementById('exportsBox');
const settingsBox = document.getElementById('settingsBox');
const message = document.getElementById('message');

function payload() {
  return {
    upstream_base_url: fields.upstreamBaseUrl.value.trim(),
    upstream_api_key: fields.upstreamApiKey.value.trim(),
    base_url: fields.baseUrl.value.trim(),
    auth_token: fields.authToken.value.trim(),
    model: fields.model.value.trim(),
    fast_model: fields.fastModel.value.trim(),
    subagent_model: fields.subagentModel.value.trim(),
    effort_level: fields.effort.value
  };
}

function quote(value) {
  return JSON.stringify(value);
}

function buildSettings(p) {
  return {
    model: p.model,
    effortLevel: p.effort_level,
    env: {
      ANTHROPIC_BASE_URL: p.base_url,
      ANTHROPIC_AUTH_TOKEN: p.auth_token,
      ANTHROPIC_MODEL: p.model,
      ANTHROPIC_DEFAULT_OPUS_MODEL: p.model,
      ANTHROPIC_DEFAULT_SONNET_MODEL: p.model,
      ANTHROPIC_DEFAULT_HAIKU_MODEL: p.fast_model,
      CLAUDE_CODE_SUBAGENT_MODEL: p.subagent_model,
      CLAUDE_CODE_EFFORT_LEVEL: p.effort_level
    }
  };
}

function buildExports(p) {
  return [
    ['ANTHROPIC_BASE_URL', p.base_url],
    ['ANTHROPIC_AUTH_TOKEN', p.auth_token],
    ['ANTHROPIC_MODEL', p.model],
    ['ANTHROPIC_DEFAULT_OPUS_MODEL', p.model],
    ['ANTHROPIC_DEFAULT_SONNET_MODEL', p.model],
    ['ANTHROPIC_DEFAULT_HAIKU_MODEL', p.fast_model],
    ['CLAUDE_CODE_SUBAGENT_MODEL', p.subagent_model],
    ['CLAUDE_CODE_EFFORT_LEVEL', p.effort_level]
  ].map(([k, v]) => 'export ' + k + '=' + quote(v)).join('\n');
}

function render() {
  const p = payload();
  exportsBox.value = buildExports(p);
  settingsBox.value = JSON.stringify(buildSettings(p), null,  2);
}

async function copyText(text) {
  await navigator.clipboard.writeText(text);
  message.textContent = 'Copied';
}

async function writeUserSettings() {
  message.textContent = 'Saving upstream and writing Claude Code settings...';
  const res = await fetch('/api/claude-config', {
    method: 'POST',
    headers: {'content-type': 'application/json'},
    body: JSON.stringify(payload())
  });
  const data = await res.json();
  if (!res.ok || !data.ok) {
    message.textContent = data.error || 'Write failed';
    return;
  }
  const backup = data.backup ? '; backup ' + data.backup : '';
  message.textContent = 'Saved upstream config to ' + data.runtime_path + '; wrote ' + data.path + backup;
}

async function init() {
  const res = await fetch('/api/claude-config');
  const data = await res.json();
  const d = data.defaults;
  fields.upstreamBaseUrl.value = d.upstream_base_url;
  fields.upstreamApiKey.placeholder = data.has_upstream_key ? 'Saved key present; leave blank to keep it' : 'Paste your upstream API key';
  fields.baseUrl.value = d.base_url;
  fields.authToken.value = d.auth_token;
  fields.model.value = d.model;
  fields.fastModel.value = d.fast_model;
  fields.subagentModel.value = d.subagent_model;
  fields.effort.value = d.effort_level;
  Object.values(fields).forEach(el => el.addEventListener('input', render));
  Object.values(fields).forEach(el => el.addEventListener('change', render));
  render();
}
init();
</script>
</body>
</html>`))
