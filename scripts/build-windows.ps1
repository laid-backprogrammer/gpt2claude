$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path "bin" | Out-Null
go build -o "bin/gpt2claude-lite.exe" .
Write-Host "Built bin/gpt2claude-lite.exe"

