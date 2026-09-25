# Builds the executable for users: dist\AudioTracker.exe. The SoundFont is
# embedded, so it runs with a double click and nothing else.
# The GitHub "Release" workflow runs this same script for every release.
param([string]$Version = "dev")
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force $dist | Out-Null
$out = Join-Path $dist "AudioTracker.exe"

go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$Version" -o $out .
if ($LASTEXITCODE -ne 0) { throw "Fallo go build" }

$mb = [math]::Round((Get-Item $out).Length / 1MB, 1)
Write-Host "OK: $out ($mb MB, version $Version)"
