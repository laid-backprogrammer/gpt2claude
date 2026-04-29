param(
  [string]$UpstreamBaseUrl = "",
  [string]$ApiKey = "",
  [string]$Model = "gpt-5.5",
  [string]$FastModel = "",
  [string]$SubagentModel = "",
  [string]$EffortLevel = "max",
  [int]$Port = 43501,
  [switch]$StartProxy
)

$ErrorActionPreference = "Stop"

function Require-Value([string]$Name, [string]$Value) {
  if ([string]::IsNullOrWhiteSpace($Value)) {
    throw "$Name is required"
  }
}

if ([string]::IsNullOrWhiteSpace($FastModel)) {
  $FastModel = $Model
}
if ([string]::IsNullOrWhiteSpace($SubagentModel)) {
  $SubagentModel = $FastModel
}

Require-Value "UpstreamBaseUrl" $UpstreamBaseUrl
Require-Value "ApiKey" $ApiKey
Require-Value "Model" $Model

$runtimeDir = Join-Path $env:USERPROFILE ".gpt2claude-lite"
$claudeDir = Join-Path $env:USERPROFILE ".claude"
$runtimePath = Join-Path $runtimeDir "config.json"
$settingsPath = Join-Path $claudeDir "settings.json"
$localBaseUrl = "http://127.0.0.1:$Port"

New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
New-Item -ItemType Directory -Force -Path $claudeDir | Out-Null

$runtimeConfig = [ordered]@{
  base_url = $UpstreamBaseUrl.TrimEnd("/")
  api_key  = $ApiKey
  model    = $Model
}
$runtimeConfig | ConvertTo-Json -Depth 8 | Set-Content -Path $runtimePath -Encoding UTF8

$settings = [ordered]@{}
if (Test-Path $settingsPath) {
  $backup = "$settingsPath.bak-$(Get-Date -Format 'yyyyMMdd-HHmmss')"
  Copy-Item $settingsPath $backup
  $existingText = Get-Content -Path $settingsPath -Raw
  if (-not [string]::IsNullOrWhiteSpace($existingText)) {
    $existing = $existingText | ConvertFrom-Json
    foreach ($property in $existing.PSObject.Properties) {
      $settings[$property.Name] = $property.Value
    }
  }
}

if (-not $settings.Contains("env") -or $null -eq $settings["env"]) {
  $settings["env"] = [ordered]@{}
}

$envMap = [ordered]@{}
if ($settings["env"] -is [System.Collections.IDictionary]) {
  foreach ($key in $settings["env"].Keys) {
    $envMap[$key] = $settings["env"][$key]
  }
} else {
  foreach ($property in $settings["env"].PSObject.Properties) {
    $envMap[$property.Name] = $property.Value
  }
}
$envMap["ANTHROPIC_BASE_URL"] = $localBaseUrl
$envMap["ANTHROPIC_AUTH_TOKEN"] = "test"
$envMap["ANTHROPIC_MODEL"] = $Model
$envMap["ANTHROPIC_DEFAULT_OPUS_MODEL"] = $Model
$envMap["ANTHROPIC_DEFAULT_SONNET_MODEL"] = $Model
$envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = $FastModel
$envMap["CLAUDE_CODE_SUBAGENT_MODEL"] = $SubagentModel
$envMap["CLAUDE_CODE_EFFORT_LEVEL"] = $EffortLevel

$settings["env"] = $envMap
$settings["model"] = $Model
$settings["effortLevel"] = $EffortLevel

$settings | ConvertTo-Json -Depth 12 | Set-Content -Path $settingsPath -Encoding UTF8

Write-Host "Saved upstream config: $runtimePath"
Write-Host "Wrote Claude Code settings: $settingsPath"
Write-Host ""
Write-Host "Claude Code will use:"
Write-Host "  ANTHROPIC_BASE_URL=$localBaseUrl"
Write-Host "  ANTHROPIC_MODEL=$Model"

if ($StartProxy) {
  $exe = Join-Path (Get-Location) "gpt2claude-lite.exe"
  if (-not (Test-Path $exe)) {
    $exe = Join-Path (Join-Path (Get-Location) "bin") "gpt2claude-lite.exe"
  }
  if (-not (Test-Path $exe)) {
    throw "gpt2claude-lite.exe not found. Build it first with: go build -o gpt2claude-lite.exe ."
  }
  Write-Host ""
  Write-Host "Starting proxy on $localBaseUrl ..."
  & $exe --host 127.0.0.1 --port $Port
}
